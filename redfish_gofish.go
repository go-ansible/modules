package modules

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-redfish/redfish"
)

// # Opt-in in-process backend (go-redfish)
//
// By default redfish_command, redfish_config and redfish_info run DMTF's
// redfishtool ON THE TARGET through the connection (redfishtool_common.go),
// so the BMC request leaves from the target's network, as it does in real
// Ansible, where the module runs on the target. That default is unchanged.
//
// A task (or the engine, for a whole run) may instead ask for the
// go-redfish client (github.com/go-redfish/redfish), called IN-PROCESS from
// the go-ansible process: the BMC request then leaves from the CONTROL
// node, the connection is not used, and no redfishtool is needed on the
// target. The choice is stated, never inferred from the connection: a
// become-wrapped or SSH connection to the control node itself would be
// indistinguishable from a remote one.
//
// The switch travels in args under RedfishBackendKey, the same shape as
// DiffModeKey and CheckModeKey (a run-wide switch that is not part of any
// module's argument_spec). The name is deliberately NOT in real Ansible's
// `_ansible_*` namespace, so that it cannot be mistaken for, or collide
// with, an internal real Ansible may define later.
const RedfishBackendKey = "_goansible_redfish_backend"

// Values of RedfishBackendKey. The empty string, and "redfishtool", are the
// default: redfishtool on the target through the connection.
const (
	RedfishBackendRedfishtool = "redfishtool"
	RedfishBackendGoRedfish   = "go-redfish"
)

// redfishBackend reads RedfishBackendKey. useGo is true only for an explicit
// "go-redfish"; any other value than "", "redfishtool" and "go-redfish" is
// refused rather than silently ignored, because a typo must not silently
// send the request from the other network.
func redfishBackend(module string, args map[string]any) (useGo bool, res Result, ok bool) {
	v, present := args[RedfishBackendKey]
	if !present || v == nil {
		return false, Result{}, true
	}
	s, isStr := v.(string)
	if !isStr {
		return false, Fail(fmt.Sprintf("%s: %s must be a string", module, RedfishBackendKey)), false
	}
	switch s {
	case "", RedfishBackendRedfishtool:
		return false, Result{}, true
	case RedfishBackendGoRedfish:
		return true, Result{}, true
	}
	return false, Fail(fmt.Sprintf("%s: %s must be %q or %q, got %q", module, RedfishBackendKey,
		RedfishBackendRedfishtool, RedfishBackendGoRedfish, s)), false
}

// redfishGoClient builds the in-process client from the module's own
// connection arguments (baseuri, username, password, auth_token, timeout,
// validate_certs, ca_path). Credentials stay in this process's memory: they
// go to go-redfish's Config, never to argv, a file or the environment.
func redfishGoClient(module string, args map[string]any, defaultTimeout int) (*redfish.Client, Result, bool) {
	baseuri := argString(args, "baseuri", "")
	if baseuri == "" {
		return nil, Fail(module + ": missing required argument: baseuri"), false
	}
	if strings.Contains(baseuri, "://") {
		return nil, Fail(module + ": baseuri must be host[:port], without a scheme"), false
	}
	username := argString(args, "username", "")
	password := argString(args, "password", "")
	token := argString(args, "auth_token", "")
	cfg := redfish.Config{
		BaseURL: "https://" + baseuri,
		Timeout: time.Duration(argInt(args, "timeout", defaultTimeout)) * time.Second,
		CAFile:  argString(args, "ca_path", ""),
	}
	switch {
	case token != "":
		cfg.Token = token
	case username != "" || password != "":
		cfg.Credentials = redfish.Static{User: username, Password: password}
	default:
		return nil, Fail(module + ": missing parameter(s) required: username and password, or auth_token"), false
	}
	if !argBool(args, "validate_certs", true) {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true} //nolint:gosec // validate_certs=false is the operator's explicit request
		cfg.HTTP = &http.Client{Transport: tr}
	}
	c, err := redfish.New(cfg)
	if err != nil {
		return nil, Fail(module + ": " + err.Error()), false
	}
	return c, Result{}, true
}

// redfishGoFail renders an error of the client as a module failure. A
// *redfish.StatusError already carries the service's own message.
func redfishGoFail(module string, err error) Result {
	var se *redfish.StatusError
	if errors.As(err, &se) {
		return Fail(module + ": " + se.Error())
	}
	return Fail(module + ": " + err.Error())
}

// redfishGoUnsupported is what a command with no go-redfish mapping yet
// answers: a loud failure naming the escape (the default backend), never a
// silent fallback to the other network path.
func redfishGoUnsupported(module, category, command string) Result {
	return Fail(fmt.Sprintf("%s: %s/%s is not available on the %s backend; omit %s to use redfishtool on the target",
		module, category, command, RedfishBackendGoRedfish, RedfishBackendKey))
}
