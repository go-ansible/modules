package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeaCommand implements Ansible's `kea_command`
// (community.general) module: submits an arbitrary named command (with
// a JSON argument payload) to an ISC Kea DHCP server's control-agent/
// server API and returns the raw JSON response, via Kea's OWN official
// `kea-shell` tool — ISC's own command-interaction utility, shipped as
// part of the Kea DHCP server distribution itself, NOT a third-party
// CLI — instead of real kea_command.py's own hand-rolled raw
// `socket.AF_UNIX` client that speaks straight to Kea's control-agent
// Unix Domain Socket.
//
// # A genuine, verified architecture difference from the real module
//
// Real kea_command.py's own module doc is explicit that it "directly
// interfaces using UDS; the HTTP wrappers are not supported" — it
// connects to a raw Unix Domain Socket (default
// `/run/kea/kea4-ctrl-socket`) and speaks Kea's own UDS protocol
// directly. `kea-shell`, by contrast, is confirmed (from kea.
// readthedocs.io's own Kea Shell chapter, read before writing this
// file) to speak Kea's HTTP Control Agent API instead — it takes
// `--host`/`--port` (not a socket path) and always goes over HTTP(S).
// This is not a guessed detail: kea-shell's own doc chapter documents
// `--host`, `--port`, `--path`, `--service`, `--auth-user`, `--auth-
// password`/`--auth-password-file`, `--timeout`, and `--ca`/`--cert`/
// `--key` for HTTPS, with NO socket-path option anywhere. This port's
// own `socket` argument (real kea_command.py's own UDS path) is
// therefore NOT forwarded to kea-shell at all — it has no equivalent
// there — and this module instead exposes host/port (see Args below)
// to address kea-shell's own HTTP Control Agent target, matching this
// project's own "if real behavior can't be replicated through this
// port's architecture, document that honestly rather than faking it"
// rule: the underlying transport genuinely differs (UDS vs. HTTP to
// the Control Agent, which in a real Kea deployment sits in front of
// the UDS socket real kea_command.py talks to directly), even though
// the JSON command/arguments/response shape kea-shell forwards is the
// exact same Kea command API real kea_command.py's own `cmd` dict
// already targets.
//
// # kea-shell's own non-interactive invocation, verified against
// # kea.readthedocs.io's own Kea Shell chapter
//
// Confirmed directly (not guessed): "Once started, the shell reads the
// parameters for the command from standard input, which are expected
// to be in JSON format" — the command NAME is a positional argument,
// its ARGUMENTS are piped via stdin as a JSON object (NOT the full
// `{"command":...,"arguments":...}` envelope kea-shell itself
// constructs — kea-shell wraps whatever JSON object it reads from
// stdin into the `arguments` field of the command it sends, per its
// own documented scripted example: `cat param.json | kea-shell --host
// 192.0.2.1 config-write > result.json`, where param.json holds only
// the parameters, not a full command envelope). This port therefore
// pipes JSON-marshaled `arguments` (or `{}` when arguments is unset —
// matching real kea_command.py's own "use `{}` to send an empty
// arguments dict/object instead of omitting it" documented option) via
// stdin, with `command` and `--service` (when given) on argv.
//
// Args: command (required) → kea-shell's own positional command name;
// arguments (optional dict) → piped as stdin JSON, `{}` when unset;
// host (this port's own substitute for real kea_command.py's own
// `socket`, since kea-shell has no socket-path concept — required,
// no default, unlike kea-shell's own "localhost" default, since this
// port has no way to know which Control Agent a caller means without
// being told); port (optional, kea-shell's own default 8000 is used
// when unset — matching kea-shell's own documented default exactly,
// not real kea_command.py's own UDS-specific default socket path);
// service (optional) → `--service`, matching real kea_command.py's own
// `--service dhcp4`-style targeting shown in its own EXAMPLES;
// rv_unchanged/rv_changed (int lists, default empty) — interpreted
// identically to real kea_command.py's own documented precedence
// (`rv_unchanged` wins over `rv_changed` when a result code is in
// both; anything in neither is an error).
//
// Deviation — `changed` semantics: real kea_command.py sets changed
// true the instant the UDS socket write succeeds (before even parsing
// the response), "to err on the safe side" per its own doc comment,
// and ONLY sets it back to false once a response IS parsed and its
// `result` code is found in rv_unchanged. This port matches that exact
// two-stage default: Changed=true until a successful parse places the
// result code in rv_unchanged.
func moduleKeaCommand(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "kea_command"
	command, err := requireString(args, "command")
	if err != nil {
		return Result{}, err
	}
	host, err := requireString(args, "host")
	if err != nil {
		return Result{}, err
	}
	rvUnchanged := argIntList(args, "rv_unchanged")
	rvChanged := argIntList(args, "rv_changed")

	argumentsPayload := "{}"
	if v, ok := args["arguments"]; ok && v != nil {
		b, jerr := json.Marshal(v)
		if jerr != nil {
			return Result{}, fmt.Errorf("%s: encoding arguments: %w", mod, jerr)
		}
		argumentsPayload = string(b)
	}

	if res, ok := keaRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}

	argv := []string{"kea-shell", "--host", host}
	if port := argInt(args, "port", 0); port > 0 {
		argv = append(argv, "--port", strconv.Itoa(port))
	}
	if service := argString(args, "service", ""); service != "" {
		argv = append(argv, "--service", service)
	}
	argv = append(argv, command)

	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	res, err := conn.Exec(ctx, strings.Join(quoted, " "), strings.NewReader(argumentsPayload))
	if err != nil {
		return Result{}, err
	}
	// Matching real kea_command.py's own "err to the safe side": once
	// the request has genuinely been sent, changed defaults true until
	// proven otherwise below.
	result := Result{Changed: true}
	if res.RC != 0 {
		return Result{Failed: true, Changed: true, Msg: mod + ": kea-shell failed: " + keaErrMsg(res)}, nil
	}

	var response map[string]any
	out := strings.TrimSpace(res.Stdout)
	if jerr := json.Unmarshal([]byte(out), &response); jerr != nil {
		return Fail(mod + ": error parsing JSON response: " + jerr.Error()), nil
	}
	rawResult, ok := response["result"]
	if !ok {
		return Fail(mod + ": bogus JSON response (missing result)"), nil
	}
	resultFloat, ok := rawResult.(float64)
	if !ok {
		return Fail(mod + ": bogus JSON response (non-integer result)"), nil
	}
	resultCode := int(resultFloat)

	result = result.WithExtra("response", response)
	if intInList(resultCode, rvUnchanged) {
		result.Changed = false
	} else if !intInList(resultCode, rvChanged) {
		return Fail(fmt.Sprintf("%s: failure result (code %d)", mod, resultCode)).WithExtra("response", response), nil
	}
	return result, nil
}

func keaRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v kea-shell"); err != nil {
		return Fail(fmt.Sprintf("%s: the kea-shell binary (ISC Kea's own official command-interaction tool, "+
			"shipped with the Kea DHCP server distribution) is required on the target and was not found in PATH "+
			"— this port shells out to it rather than speaking Kea's UDS control-agent protocol directly; see "+
			"moduleKeaCommand's own doc comment, including the architecture difference (HTTP Control Agent via "+
			"kea-shell vs. real kea_command.py's own raw Unix Domain Socket)", moduleName)), false
	}
	return Result{}, true
}

func keaErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}

// argIntList is defined in mas.go and reused here as-is.

func intInList(n int, list []int) bool {
	for _, v := range list {
		if v == n {
			return true
		}
	}
	return false
}
