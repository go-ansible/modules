package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSssdInfo implements Ansible's `sssd_info` module: checks SSSD
// (System Security Services Daemon) domain status, lists configured
// domains, and retrieves active/known servers — read-only, matching
// real sssd_info.py's own source (plugins/modules/sssd_info.py), which
// does all of this over D-Bus, talking directly to SSSD's own
// `org.freedesktop.sssd.infopipe` system-bus service (via the `dbus`
// Python library, a hard requirement of the real module).
//
// This port has no D-Bus client library available (CGO_ENABLED=0, and
// no pure-Go D-Bus dependency was added for a single module — see this
// batch's own scope), so rather than skip the module or fake a
// probe-free result, it shells out to `busctl` (systemd's own D-Bus CLI,
// widely available alongside SSSD on the same systemd-based Linux
// distributions SSSD itself targets) to make the IDENTICAL D-Bus method
// calls real sssd_info's own SSSDHandler class makes, verified directly
// from its source: bus name `org.freedesktop.sssd.infopipe`, root object
// `/org/freedesktop/sssd/infopipe` with interface
// `org.freedesktop.sssd.infopipe` for `ListDomains()`, and per-domain
// object `/org/freedesktop/sssd/infopipe/Domains/<escaped-domain>` with
// interface `org.freedesktop.sssd.infopipe.Domains.Domain` for
// `IsOnline()`/`ActiveServer(s)`/`ListServers(s)`.
//
// ⚠ Domain-name path escaping matches real sssd_info's own
// `domain.replace('.', '_2e')` EXACTLY, including its own simplification
// (a real D-Bus object-path escape replaces every non-alphanumeric byte
// with "_XX", but real sssd_info only ever substitutes literal dots —
// this port replicates that exact, narrower behavior rather than a more
// "correct" general escape, since a domain name containing any other
// D-Bus-unsafe character would break identically against a real SSSD
// server through real sssd_info too).
//
// ⚠ DEVIATION vs real sssd_info, honestly flagged per this batch's own
// hard rule: this port could not verify `busctl call --json=short`'s
// exact JSON reply shape against a LIVE SSSD/D-Bus system (no such
// environment was available to this batch) — the parsing below assumes
// systemd's own documented `{"type":"...","data":[...]}` envelope with
// the method's return value(s) as `data`'s own elements, which is
// standard busctl behavior, but was not confirmed against this specific
// D-Bus service's real wire replies. If SSSD's infopipe wraps its
// return values in a shape busctl represents differently than assumed
// here, this module will fail cleanly (Result{Failed:true}, from a
// JSON/type-assertion error) rather than silently misreporting.
//
// Args: action (required; domain_status|domain_list|active_servers|
// list_servers); domain (required unless action=domain_list, matching
// real sssd_info's own required_if); server_type (choices IPA|AD,
// required when action is active_servers or list_servers, matching real
// sssd_info's own required_if — ignored otherwise).
//
// Never Changed — this module only ever reads (matching real
// sssd_info's own supports_check_mode with no state-changing action).
func moduleSssdInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	action := argString(args, "action", "")
	domain := argString(args, "domain", "")
	serverType := argString(args, "server_type", "")

	switch action {
	case "domain_status":
		if domain == "" {
			return Result{}, errArg("sssd_info: domain is required when action=domain_status")
		}
	case "domain_list":
		// domain is ignored for this action, matching real sssd_info.
	case "active_servers", "list_servers":
		if domain == "" {
			return Result{}, errArg("sssd_info: domain is required when action=%s", action)
		}
		if serverType != "IPA" && serverType != "AD" {
			return Result{}, errArg("sssd_info: server_type must be IPA or AD when action=%s, got %q", action, serverType)
		}
	default:
		return Result{}, errArg("sssd_info: action must be domain_status, domain_list, active_servers, or list_servers, got %q", action)
	}

	if _, err := run(ctx, conn, "command -v busctl"); err != nil {
		return Fail("sssd_info: the busctl binary (from systemd) is required on the target and was not " +
			"found in PATH — this port shells out to it to make the same D-Bus calls real sssd_info's own " +
			"dbus-python client makes; see sssd_info.go's own doc comment"), nil
	}

	switch action {
	case "domain_list":
		reply, res, err := sssdBusctlCall(ctx, conn, sssdInfopipePath, sssdInfopipeIface, "ListDomains", "")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return sssdFailed("ListDomains", res), nil
		}
		paths, err := sssdReplyStringArray(reply)
		if err != nil {
			return Fail("sssd_info: ListDomains: " + err.Error()), nil
		}
		domains := make([]string, len(paths))
		for i, p := range paths {
			domains[i] = sssdDomainNameFromPath(p)
		}
		return Ok("").WithExtra("domain_list", domains), nil

	case "domain_status":
		obj := sssdDomainObjectPath(domain)
		reply, res, err := sssdBusctlCall(ctx, conn, obj, sssdDomainIface, "IsOnline", "")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return sssdFailed("IsOnline", res), nil
		}
		online, err := sssdReplyBool(reply)
		if err != nil {
			return Fail("sssd_info: IsOnline: " + err.Error()), nil
		}
		status := "offline"
		if online {
			status = "online"
		}
		return Ok("").WithExtra("online", status), nil

	case "active_servers":
		obj := sssdDomainObjectPath(domain)
		servers := map[string]any{}
		if serverType == "IPA" {
			v, res, err := sssdActiveServer(ctx, conn, obj, "IPA")
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return sssdFailed("ActiveServer", res), nil
			}
			servers["IPA Server"] = v
		} else {
			gc, res, err := sssdActiveServer(ctx, conn, obj, "sd_gc_"+domain)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return sssdFailed("ActiveServer", res), nil
			}
			dc, res, err := sssdActiveServer(ctx, conn, obj, "sd_"+domain)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return sssdFailed("ActiveServer", res), nil
			}
			servers["AD Global Catalog"] = gc
			servers["AD Domain Controller"] = dc
		}
		return Ok("").WithExtra("servers", servers), nil

	case "list_servers":
		obj := sssdDomainObjectPath(domain)
		arg := serverType
		if serverType != "IPA" {
			arg = "sd_" + domain
		}
		reply, res, err := sssdBusctlCall(ctx, conn, obj, sssdDomainIface, "ListServers", "s", arg)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return sssdFailed("ListServers", res), nil
		}
		list, err := sssdReplyStringArray(reply)
		if err != nil {
			return Fail("sssd_info: ListServers: " + err.Error()), nil
		}
		return Ok("").WithExtra("list_servers", list), nil
	}

	return Result{}, errArg("sssd_info: unreachable action %q", action)
}

const (
	sssdBusName       = "org.freedesktop.sssd.infopipe"
	sssdInfopipePath  = "/org/freedesktop/sssd/infopipe"
	sssdInfopipeIface = "org.freedesktop.sssd.infopipe"
	sssdDomainIface   = "org.freedesktop.sssd.infopipe.Domains.Domain"
)

// sssdDomainObjectPath builds a domain's own D-Bus object path, matching
// real sssd_info's own (simplified) escaping — see moduleSssdInfo's own
// doc comment.
func sssdDomainObjectPath(domain string) string {
	return sssdInfopipePath + "/Domains/" + strings.ReplaceAll(domain, ".", "_2e")
}

// sssdDomainNameFromPath reverses sssdDomainObjectPath for one path
// element returned by ListDomains, matching real sssd_info's own
// `domain.rsplit("/", maxsplit=1)[-1].replace("_2e", ".")`.
func sssdDomainNameFromPath(path string) string {
	name := path
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		name = path[i+1:]
	}
	return strings.ReplaceAll(name, "_2e", ".")
}

// sssdActiveServer calls ActiveServer(s) and returns its single string
// reply — shared by the "IPA" and "AD" branches of action=active_servers.
func sssdActiveServer(ctx context.Context, conn remoteexec.Connection, obj, arg string) (string, remoteexec.Result, error) {
	reply, res, err := sssdBusctlCall(ctx, conn, obj, sssdDomainIface, "ActiveServer", "s", arg)
	if err != nil || res.RC != 0 {
		return "", res, err
	}
	v, err := sssdReplyString(reply)
	if err != nil {
		return "", res, fmt.Errorf("ActiveServer: %w", err)
	}
	return v, res, nil
}

// sssdBusctlReply is systemd's own busctl `--json=short` reply envelope
// for `call` — see moduleSssdInfo's own doc comment for the honest
// caveat on this shape.
type sssdBusctlReply struct {
	Type string `json:"type"`
	Data []any  `json:"data"`
}

// sssdBusctlCall runs one `busctl --system --json=short call` invocation
// and parses its JSON reply. A non-zero exit is returned via res (for
// the caller to build a Fail()), not as a Go error.
func sssdBusctlCall(ctx context.Context, conn remoteexec.Connection, objPath, iface, method, argSig string, callArgs ...string) (sssdBusctlReply, remoteexec.Result, error) {
	parts := []string{"busctl", "--system", "--json=short", "call", sssdBusName, objPath, iface, method}
	if argSig != "" {
		parts = append(parts, argSig)
		parts = append(parts, callArgs...)
	}
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = shellQuote(p)
	}
	res, err := runStatus(ctx, conn, strings.Join(quoted, " "))
	if err != nil || res.RC != 0 {
		return sssdBusctlReply{}, res, err
	}
	var reply sssdBusctlReply
	if err := json.Unmarshal([]byte(strings.TrimSpace(res.Stdout)), &reply); err != nil {
		return sssdBusctlReply{}, res, fmt.Errorf("parsing busctl JSON reply: %w", err)
	}
	return reply, res, nil
}

func sssdFailed(method string, res remoteexec.Result) Result {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return Fail(fmt.Sprintf("sssd_info: %s: %s", method, msg))
}

func sssdReplyBool(r sssdBusctlReply) (bool, error) {
	if len(r.Data) == 0 {
		return false, fmt.Errorf("empty reply")
	}
	b, ok := r.Data[0].(bool)
	if !ok {
		return false, fmt.Errorf("reply data[0] = %#v, want a bool", r.Data[0])
	}
	return b, nil
}

func sssdReplyString(r sssdBusctlReply) (string, error) {
	if len(r.Data) == 0 {
		return "", fmt.Errorf("empty reply")
	}
	s, ok := r.Data[0].(string)
	if !ok {
		return "", fmt.Errorf("reply data[0] = %#v, want a string", r.Data[0])
	}
	return s, nil
}

func sssdReplyStringArray(r sssdBusctlReply) ([]string, error) {
	if len(r.Data) == 0 {
		return nil, fmt.Errorf("empty reply")
	}
	items, ok := r.Data[0].([]any)
	if !ok {
		return nil, fmt.Errorf("reply data[0] = %#v, want an array", r.Data[0])
	}
	out := make([]string, len(items))
	for i, it := range items {
		s, ok := it.(string)
		if !ok {
			return nil, fmt.Errorf("reply data[0][%d] = %#v, want a string", i, it)
		}
		out[i] = s
	}
	return out, nil
}
