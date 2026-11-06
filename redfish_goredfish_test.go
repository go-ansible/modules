package modules

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/go-redfish/redfish/mockbmc"
)

// These tests need no ansible: they drive the in-process backend against
// go-redfish's own mock BMC (both trees). redfish_interop_test.go compares
// it with real ansible.

func goBackendArgs(b *interopBMC, extra map[string]any) map[string]any {
	a := map[string]any{"baseuri": b.baseuri, "username": "admin", "password": "password",
		"ca_path": b.caFile, RedfishBackendKey: RedfishBackendGoRedfish}
	for k, v := range extra {
		a[k] = v
	}
	return a
}

func TestGoRedfishBackendBothTrees(t *testing.T) {
	for _, tree := range []string{mockbmc.TreeRackmount, mockbmc.TreeEmulator} {
		t.Run(tree, func(t *testing.T) {
			b := startInteropBMC(t, mockbmc.Options{Tree: tree})
			// connection is nil: the backend must never touch it.
			res, err := moduleRedfishInfo(context.Background(), nil, goBackendArgs(b, map[string]any{
				"category": []any{"Systems"}, "command": []any{"GetSystemInventory"}}))
			if err != nil || res.Failed {
				t.Fatalf("info: %+v %v", res, err)
			}
			if _, ok := res.Extra["redfish_facts"].(map[string]any)["system"]; !ok {
				t.Fatalf("no system fact: %+v", res.Extra)
			}
			res, err = moduleRedfishCommand(context.Background(), nil, goBackendArgs(b, map[string]any{
				"category": "Systems", "command": []any{"PowerForceOff"}}))
			if err != nil || res.Failed || !res.Changed {
				t.Fatalf("command: %+v %v", res, err)
			}
			res, err = moduleRedfishCommand(context.Background(), nil, goBackendArgs(b, map[string]any{
				"category": "Systems", "command": []any{"PowerForceOff"}}))
			if err != nil || res.Failed || res.Changed {
				t.Fatalf("second PowerForceOff must be unchanged: %+v %v", res, err)
			}
			res, err = moduleRedfishConfig(context.Background(), nil, goBackendArgs(b, map[string]any{
				"category": "Systems", "command": []any{"SetPowerRestorePolicy"}, "power_restore_policy": "AlwaysOn"}))
			if err != nil || res.Failed {
				t.Fatalf("config: %+v %v", res, err)
			}
		})
	}
}

func TestGoRedfishBackendAuthToken(t *testing.T) {
	b := startInteropBMC(t, mockbmc.Options{})
	// open a session on the mock, then use only its token.
	cl := b.srv.Client()
	resp, err := cl.Post(b.srv.URL+"/redfish/v1/SessionService/Sessions", "application/json",
		strings.NewReader(`{"UserName":"admin","Password":"password"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	tok := resp.Header.Get("X-Auth-Token")
	if tok == "" {
		t.Fatal("the mock gave no token")
	}
	args := goBackendArgs(b, map[string]any{"category": []any{"Service"}, "command": []any{"CheckAvailability"},
		"auth_token": tok})
	delete(args, "username")
	delete(args, "password")
	args["category"], args["command"] = []any{"Systems"}, []any{"GetSystemInventory"}
	res, err := moduleRedfishInfo(context.Background(), nil, args)
	if err != nil || res.Failed {
		t.Fatalf("token auth: %+v %v", res, err)
	}
}

func TestRedfishBackendSelector(t *testing.T) {
	for _, tc := range []struct {
		val  any
		useG bool
		fail bool
	}{
		{nil, false, false}, {"", false, false}, {"redfishtool", false, false}, {"go-redfish", true, false},
		{"gofish", false, true}, {"GO-REDFISH", false, true}, {true, false, true},
	} {
		args := map[string]any{}
		if tc.val != nil {
			args[RedfishBackendKey] = tc.val
		}
		use, res, ok := redfishBackend("redfish_info", args)
		if use != tc.useG || (!ok) != tc.fail || (!ok && !res.Failed) {
			t.Errorf("value %#v: use=%v ok=%v res=%+v", tc.val, use, ok, res)
		}
	}
}

func TestRedfishDefaultBackendRefusesTokenAndPointsAtGoRedfish(t *testing.T) {
	_, _, _, res, ok := redfishtoolCredentials("redfish_info", map[string]any{"baseuri": "bmc", "auth_token": "x"})
	if ok || !res.Failed {
		t.Fatalf("default path must still refuse auth_token: %+v", res)
	}
	if !strings.Contains(res.Msg, RedfishBackendKey) || !strings.Contains(res.Msg, RedfishBackendGoRedfish) {
		t.Errorf("refusal must name the new backend: %q", res.Msg)
	}
}

// Real's three credential constraints, with its wording, refused alike by
// both backends -- the backend switch must change where the request leaves
// from and nothing else. The username+auth_token case is the one v0.98.0
// got wrong on the go-redfish path: the token silently won.
func TestRedfishCredentialConstraintsOnBothBackends(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"username and auth_token", map[string]any{"username": "u", "password": "p", "auth_token": "t"},
			"parameters are mutually exclusive: username|auth_token"},
		{"username without password", map[string]any{"username": "u"},
			"parameters are required together: username, password"},
		{"password without username", map[string]any{"password": "p"},
			"parameters are required together: username, password"},
		{"neither", map[string]any{},
			"one of the following is required: username, auth_token"},
		{"an empty username is given", map[string]any{"username": ""},
			"parameters are required together: username, password"},
		{"baseuri with a scheme", map[string]any{"baseuri": "https://bmc", "username": "u", "password": "p"},
			"redfish_info: baseuri must be host[:port], without a scheme"},
		// Real checks mutually_exclusive BEFORE the required arguments.
		{"username+auth_token and no baseuri", map[string]any{"baseuri": nil, "username": "u", "password": "p", "auth_token": "t"},
			"parameters are mutually exclusive: username|auth_token"},
	}
	for _, tc := range cases {
		args := map[string]any{"baseuri": "bmc", "category": []any{"Systems"}, "command": []any{"GetSystemInventory"}}
		for k, v := range tc.args {
			args[k] = v
		}
		_, _, _, res, ok := redfishtoolCredentials("redfish_info", args)
		if ok || res.Msg != tc.want {
			t.Errorf("redfishtool path, %s: ok=%v msg=%q, want %q", tc.name, ok, res.Msg, tc.want)
		}
		_, res, ok = redfishGoClient("redfish_info", args, 60)
		if ok || res.Msg != tc.want {
			t.Errorf("go-redfish path, %s: ok=%v msg=%q, want %q", tc.name, ok, res.Msg, tc.want)
		}
	}
	// The control: a well-formed set passes both checks.
	good := map[string]any{"baseuri": "bmc", "username": "u", "password": "p"}
	if _, _, _, res, ok := redfishtoolCredentials("redfish_info", good); !ok {
		t.Errorf("redfishtool path refused a good set: %q", res.Msg)
	}
	if _, res, ok := redfishGoClient("redfish_info", good, 60); !ok {
		t.Errorf("go-redfish path refused a good set: %q", res.Msg)
	}
}

func TestAcceptsRedfishBackend(t *testing.T) {
	for name, want := range map[string]bool{
		"redfish_info": true, "community.general.redfish_command": true, "redfish_config": true,
		"command": false, "idrac_redfish_info": false, "": false,
	} {
		if got := AcceptsRedfishBackend(name); got != want {
			t.Errorf("AcceptsRedfishBackend(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestGoRedfishBackendCertificateIsVerified(t *testing.T) {
	b := startInteropBMC(t, mockbmc.Options{})
	args := goBackendArgs(b, map[string]any{"category": []any{"Systems"}, "command": []any{"GetSystemInventory"}})
	delete(args, "ca_path") // the mock's certificate is self-signed: not trusted
	res, err := moduleRedfishInfo(context.Background(), nil, args)
	if err != nil || !res.Failed {
		t.Fatalf("an untrusted certificate must fail: %+v %v", res, err)
	}
	args["validate_certs"] = false
	res, err = moduleRedfishInfo(context.Background(), nil, args)
	if err != nil || res.Failed {
		t.Fatalf("validate_certs=false must go through: %+v %v", res, err)
	}
}

func TestGoRedfishBackendNeverPutsCredentialsInTheEnvironmentOrOnTheWire(t *testing.T) {
	// The password must reach the server only as the Authorization header of
	// an https request: nothing in the URL.
	var seenURL string
	b := startInteropBMC(t, mockbmc.Options{})
	h := b.srv.Config.Handler
	b.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenURL += r.URL.String() + "\n"
		h.ServeHTTP(w, r)
	})
	args := goBackendArgs(b, map[string]any{"password": "s3cr3t-pw", "category": []any{"Systems"}, "command": []any{"GetSystemInventory"}})
	_, _ = moduleRedfishInfo(context.Background(), nil, args)
	if strings.Contains(seenURL, "s3cr3t-pw") {
		t.Fatalf("password in a URL: %s", seenURL)
	}
}

func TestGoRedfishBackendUnknownCommandWording(t *testing.T) {
	res, _ := moduleRedfishCommand(context.Background(), nil, map[string]any{
		"category": "Systems", "command": []any{"Nope"}, "baseuri": "x", "username": "u", "password": "p",
		RedfishBackendKey: RedfishBackendGoRedfish})
	if !res.Failed || !strings.HasPrefix(res.Msg, "Invalid Command 'Nope'. Valid Commands = ['PowerOn'") {
		t.Fatalf("msg = %q", res.Msg)
	}
}
