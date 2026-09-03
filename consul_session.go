package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleConsulSession implements Ansible's `consul_session`
// (community.general) module: creates, destroys, or queries a Consul
// session.
//
// Unlike every other consul_*.go module in this port, this one does NOT
// shell out to the `consul` CLI: HashiCorp's own command reference (the
// full top-level command tree at developer.hashicorp.com/consul/commands
// was checked directly for this module) has NO `consul session` — or any
// other — subcommand family at all; Consul sessions are reachable ONLY
// through the HTTP API's own /v1/session/* endpoints, which real
// consul_session's python-consul client calls directly. Since this port
// has no Go Consul client wired into remoteexec.Connection either, it
// substitutes a remote `curl` invocation against that same HTTP API —
// the same "compose the request as curl via conn.Exec" approach uri.go
// already documents and this file reuses (parseCurlStatus) — rather than
// silently refusing to implement a module with no CLI substitution
// available, or faking behavior this port cannot actually perform.
//
// Args: state (present|absent|info|list|node, default present);
// name (session name, state=present); node (the associated node at
// creation; also the node NAME this port queries by for state=node —
// real consul_session's own doc is unclear on which of `name`/session
// `id` a state=node lookup key is; `node` was chosen as the more
// specific, purpose-built argument); checks ([]string); delay (int
// seconds, default 15, sent as Consul's own "<n>s" LockDelay); behavior
// (release|delete, default release); ttl (int seconds, sent as "<n>s");
// id (required for state=absent/info); datacenter (sent as a `?dc=`
// query parameter); host/port/scheme/ca_path/token (sent as an
// `X-Consul-Token` header, matching real python-consul's own header,
// rather than CONSUL_HTTP_TOKEN — there is no `consul` CLI invocation
// here for an environment variable to hide it from)/validate_certs
// (false adds curl's own -k).
//
// state=present always creates a brand NEW session and reports
// Changed=true — sessions have no natural key to compare against a
// prior run by, matching real consul_session's own behavior (it always
// calls session.create, never checks for an existing one first).
// state=absent destroys by id; Changed reflects the API's own
// true/false destroy response body. state=info/list/node are read-only
// (Extra["sessions"], never Changed).
func moduleConsulSession(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state := argString(args, "state", "present")

	switch state {
	case "present":
		behavior := argString(args, "behavior", "release")
		if behavior != "release" && behavior != "delete" {
			return Result{}, errArg("consul_session: behavior must be release or delete, got %q", behavior)
		}
		body := map[string]any{"Behavior": behavior, "LockDelay": strconv.Itoa(argInt(args, "delay", 15)) + "s"}
		if n := argString(args, "name", ""); n != "" {
			body["Name"] = n
		}
		if n := argString(args, "node", ""); n != "" {
			body["Node"] = n
		}
		if checks := argStringList(args, "checks"); len(checks) > 0 {
			body["Checks"] = checks
		}
		if ttl := argInt(args, "ttl", 0); ttl != 0 {
			body["TTL"] = strconv.Itoa(ttl) + "s"
		}
		b, err := json.Marshal(body)
		if err != nil {
			return Result{}, err
		}
		respBody, status, err := consulSessionRequest(ctx, conn, args, "PUT", "/v1/session/create", string(b))
		if err != nil {
			return Result{}, err
		}
		if status != 200 {
			return Fail("consul_session: create failed: " + respBody), nil
		}
		var parsed struct {
			ID string
		}
		if err := json.Unmarshal([]byte(respBody), &parsed); err != nil {
			return Result{}, fmt.Errorf("consul_session: decoding create response: %w", err)
		}
		return Changed("").WithExtra("id", parsed.ID), nil

	case "absent":
		id, err := requireString(args, "id")
		if err != nil {
			return Result{}, err
		}
		respBody, status, err := consulSessionRequest(ctx, conn, args, "PUT", "/v1/session/destroy/"+id, "")
		if err != nil {
			return Result{}, err
		}
		if status != 200 {
			return Fail("consul_session: destroy failed: " + respBody), nil
		}
		return Result{Changed: strings.TrimSpace(respBody) == "true"}, nil

	case "info":
		id, err := requireString(args, "id")
		if err != nil {
			return Result{}, err
		}
		return consulSessionList(ctx, conn, args, "/v1/session/info/"+id)

	case "list":
		return consulSessionList(ctx, conn, args, "/v1/session/list")

	case "node":
		node, err := requireString(args, "node")
		if err != nil {
			return Result{}, err
		}
		return consulSessionList(ctx, conn, args, "/v1/session/node/"+node)

	default:
		return Result{}, errArg("consul_session: state must be one of present, absent, info, list, node, got %q", state)
	}
}

// consulSessionList issues a GET against path and decodes a JSON array
// of session objects into Extra["sessions"], shared by state=info/list/
// node.
func consulSessionList(ctx context.Context, conn remoteexec.Connection, args map[string]any, path string) (Result, error) {
	respBody, status, err := consulSessionRequest(ctx, conn, args, "GET", path, "")
	if err != nil {
		return Result{}, err
	}
	if status != 200 {
		return Fail("consul_session: request failed: " + respBody), nil
	}
	var sessions []map[string]any
	if err := json.Unmarshal([]byte(respBody), &sessions); err != nil {
		return Result{}, fmt.Errorf("consul_session: decoding response: %w", err)
	}
	if sessions == nil {
		sessions = []map[string]any{}
	}
	return Ok("").WithExtra("sessions", sessions), nil
}

// consulSessionRequest issues one curl request against the Consul HTTP
// API's own session endpoints, reusing uri.go's own parseCurlStatus to
// split the marker-tagged response — see this module's own doc comment
// for why a curl substitution (rather than the `consul` CLI every other
// consul_*.go module in this port shells out to) is used here.
func consulSessionRequest(ctx context.Context, conn remoteexec.Connection, args map[string]any, method, path, body string) (respBody string, status int, err error) {
	scheme := argString(args, "scheme", "http")
	host := argString(args, "host", "localhost")
	port := argInt(args, "port", 8500)
	url := scheme + "://" + host + ":" + strconv.Itoa(port) + path
	if dc := argString(args, "datacenter", ""); dc != "" {
		url += "?dc=" + dc
	}

	var b strings.Builder
	b.WriteString("curl -s -w " + shellQuote("\nHTTPSTATUS:%{http_code}") + " -X " + shellQuote(method))
	if tok := argString(args, "token", ""); tok != "" {
		b.WriteString(" -H " + shellQuote("X-Consul-Token: "+tok))
	}
	if !argBool(args, "validate_certs", true) {
		b.WriteString(" -k")
	}
	if ca := argString(args, "ca_path", ""); ca != "" {
		b.WriteString(" --capath " + shellQuote(ca))
	}
	if body != "" {
		b.WriteString(" -d " + shellQuote(body))
	}
	b.WriteString(" " + shellQuote(url))

	res, err := runStatus(ctx, conn, b.String())
	if err != nil {
		return "", 0, err
	}
	if res.RC != 0 {
		return "", 0, fmt.Errorf("consul_session: curl failed: %s", strings.TrimSpace(res.Stderr))
	}
	respBody, status, err = parseCurlStatus(res.Stdout)
	if err != nil {
		return "", 0, fmt.Errorf("consul_session: %w", err)
	}
	return respBody, status, nil
}
