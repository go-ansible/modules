package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// kopiaStateMap matches real module_utils/_kopia.py's own STATE_MAP:
// each kopia_repository state maps to its own `kopia repository`
// subcommand.
var kopiaStateMap = map[string]string{
	"created":      "create",
	"connected":    "connect",
	"disconnected": "disconnect",
	"synced":       "sync-to",
	"throttled":    "throttle",
}

// kopiaIgnoreErr maps each state (except throttled — see below) to the
// stderr substring real kopia_repository's own output_process treats
// as "already in the desired state, not a real failure": state=created
// "already exists", state=connected "already connected",
// state=disconnected "does not exist", state=synced "already synced".
var kopiaIgnoreErr = map[string]string{
	"created":      "already exists",
	"connected":    "already connected",
	"disconnected": "does not exist",
	"synced":       "already synced",
}

// kopiaFlag pairs one backend/throttle sub-argument name with the CLI
// flag it becomes, preserving the same order real
// module_utils/_kopia.py's own _PROVIDER_BACKEND_MAP/_fmt_throttle
// dicts iterate in (Python dicts preserve insertion order; a Go map
// does not, hence this ordered-slice shape).
type kopiaFlag struct{ key, flag string }

// kopiaBackendFlags matches real _PROVIDER_BACKEND_MAP exactly, but
// only for the sub-arguments kopia_repository.py's own argument_spec
// actually declares (embed_credentials/client_id/rclone_exe/
// sftp_password/etc. exist in the shared _kopia.py table for other,
// not-yet-ported callers, but are never reachable through this
// module's own args, so they are omitted here — see moduleKopiaRepository's
// own doc comment).
var kopiaBackendFlags = map[string][]kopiaFlag{
	"azure": {
		{"container", "--container"},
		{"storage_account", "--storage-account"},
		{"storage_key", "--storage-key"},
		{"sas_token", "--sas-token"},
		{"storage_domain", "--storage-domain"},
		{"prefix", "--prefix"},
	},
	"b2": {
		{"bucket", "--bucket"},
		{"access_key", "--key-id"},
		{"secret_access_key", "--key"},
		{"prefix", "--prefix"},
	},
	"filesystem": {
		{"path", "--path"},
	},
	"gcs": {
		{"bucket", "--bucket"},
		{"credentials_file", "--credentials-file"},
		{"prefix", "--prefix"},
	},
	"gdrive": {
		{"folder_id", "--folder-id"},
		{"credentials_file", "--credentials-file"},
	},
	"rclone": {
		{"path", "--remote-path"},
	},
	"s3": {
		{"bucket", "--bucket"},
		{"access_key", "--access-key"},
		{"secret_access_key", "--secret-access-key"},
		{"endpoint", "--endpoint"},
		{"region", "--region"},
		{"prefix", "--prefix"},
		{"session_token", "--session-token"},
	},
	"sftp": {
		{"path", "--path"},
		{"host", "--host"},
		{"username", "--username"},
		{"port", "--port"},
		{"keyfile", "--keyfile"},
		{"known_hosts", "--known-hosts"},
	},
	"webdav": {
		{"url", "--url"},
		{"webdav_username", "--webdav-username"},
		{"webdav_password", "--webdav-password"},
	},
}

// kopiaBackendRequired matches real kopia_repository.py's own
// backend.required_if list.
var kopiaBackendRequired = map[string][]string{
	"azure":      {"container", "storage_account"},
	"b2":         {"bucket", "access_key", "secret_access_key"},
	"filesystem": {"path"},
	"gcs":        {"bucket"},
	"gdrive":     {"folder_id"},
	"rclone":     {"path"},
	"s3":         {"bucket", "access_key", "secret_access_key"},
	"sftp":       {"path", "host", "username"},
	"webdav":     {"url"},
}

// kopiaThrottleFlags matches real module_utils/_kopia.py's own
// _fmt_throttle flag_map, in the same order.
var kopiaThrottleFlags = []kopiaFlag{
	{"download_bytes_per_second", "--download-bytes-per-second"},
	{"upload_bytes_per_second", "--upload-bytes-per-second"},
	{"read_requests_per_second", "--read-requests-per-second"},
	{"write_requests_per_second", "--write-requests-per-second"},
	{"list_requests_per_second", "--list-requests-per-second"},
	{"concurrent_reads", "--concurrent-reads"},
	{"concurrent_writes", "--concurrent-writes"},
}

// moduleKopiaRepository implements Ansible's `kopia_repository`
// (community.general) module: creates, connects to, disconnects from,
// syncs, or throttles a Kopia backup repository via the `kopia` CLI —
// read from real kopia_repository.py's own KopiaRepository state_*
// methods and module_utils/_kopia.py's own fmt_backend/_fmt_throttle/
// kopia_runner (this batch's hard rule: the exact per-provider flag
// names/order and the status-diff idempotency check are only visible
// there, not EXAMPLES/OPTIONS).
//
// Args: state (created|connected|disconnected|synced|throttled,
// default created); password; config (--config-file); backend (a
// dict, required for created/connected/synced) with "provider"
// (required: azure|b2|filesystem|gcs|gdrive|rclone|s3|sftp|webdav|
// server) plus the provider-specific sub-arguments kopiaBackendFlags
// lists (this port only implements the subset kopia_repository.py's
// own argument_spec actually declares — see kopiaBackendFlags's own
// doc comment); fingerprint_tls/url (connected+backend.provider=server
// only: --server-cert-fingerprint/--url); throttle (a dict, throttled
// only) with kopiaThrottleFlags's own keys.
//
// Backend argv shape: [provider, --flag1=value1, --flag2=value2, ...]
// for every provider except "server", which real fmt_backend() returns
// [] for — real kopia_repository's own comment explains this as
// "server connect uses top-level flags instead of backend flags", but
// the actual effect (reproduced here exactly, not fixed) is that NO
// "server" token is ever emitted either, only --server-cert-fingerprint/
// --url; this looks like an upstream gap, not an intentional design,
// but this port replicates it rather than silently correcting upstream
// behavior it was not asked to fix.
//
// Idempotency: unlike every other module in this port, kopia_repository
// does not compare a before/desired value pair — it compares
// `kopia repository status --config-file=...`'s own trimmed stdout
// BEFORE and AFTER running the state's own command (ignoring rc — a
// disconnected repository's status exits non-zero, which real _get()
// treats as informational, not an error), and reports Changed if that
// text differs at all. A state command's own non-zero exit is ignored
// (not a failure) when its stderr contains the state's own
// kopiaIgnoreErr substring (e.g. "already exists" for state=created) —
// matching real _process_command_output's own fail_on_err/ignore_err_msg
// gate — and otherwise fails.
//
// Deviation: real state_throttled's own output_process call omits
// ignore_err_msg, defaulting to "" — and because Python's `x not in s`
// is False whenever x=="" (the empty string is a substring of every
// string), that default silently makes real state_throttled NEVER
// raise on ANY command failure, an apparent upstream oversight rather
// than intended behavior. This port does not reproduce that: state=
// throttled here fails on any non-zero exit, the same as every other
// state's genuine (non-"already done") errors do.
//
// Extra: kopia_repository (the repository status text after this run,
// matching real kopia_repository's own RETURN VALUES key of the same
// name).
func moduleKopiaRepository(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state := argString(args, "state", "created")
	subcmd, ok := kopiaStateMap[state]
	if !ok {
		return Result{}, errArg("kopia_repository: state must be one of created, connected, disconnected, synced, throttled, got %q", state)
	}

	config := argString(args, "config", "")
	password := argString(args, "password", "")

	backendRaw, hasBackend := args["backend"].(map[string]any)
	if (state == "created" || state == "connected" || state == "synced") && !hasBackend {
		return Result{}, errArg("kopia_repository: backend is required when state=%s", state)
	}

	var backendArgv []string
	var provider string
	if hasBackend {
		provider = argString(backendRaw, "provider", "")
		var err error
		backendArgv, err = kopiaBackendArgv(backendRaw)
		if err != nil {
			return Result{}, err
		}
	}

	if state == "connected" && provider == "server" {
		if argString(args, "url", "") == "" {
			return Result{}, errArg("kopia_repository: url is required when state=connected and backend.provider=server")
		}
		if argString(args, "fingerprint_tls", "") == "" {
			return Result{}, errArg("kopia_repository: fingerprint_tls is required when state=connected and backend.provider=server")
		}
	}

	before, err := kopiaStatus(ctx, conn, config)
	if err != nil {
		return Result{}, err
	}

	var argv []string
	switch state {
	case "created", "synced":
		argv = append([]string{"repository", subcmd}, backendArgv...)
		if password != "" {
			argv = append(argv, "--password="+password)
		}
	case "connected":
		argv = append([]string{"repository", subcmd}, backendArgv...)
		if password != "" {
			argv = append(argv, "--password="+password)
		}
		if ft := argString(args, "fingerprint_tls", ""); ft != "" {
			argv = append(argv, "--server-cert-fingerprint="+ft)
		}
		if u := argString(args, "url", ""); u != "" {
			argv = append(argv, "--url="+u)
		}
	case "disconnected":
		argv = []string{"repository", subcmd}
		if password != "" {
			argv = append(argv, "--password="+password)
		}
	case "throttled":
		throttle, _ := args["throttle"].(map[string]any)
		argv = append([]string{"repository", subcmd, "set"}, kopiaThrottleArgv(throttle)...)
	}
	if config != "" {
		argv = append(argv, "--config-file="+config)
	}

	res, err := kopiaRun(ctx, conn, argv)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		ignore := kopiaIgnoreErr[state]
		stderr := strings.TrimSpace(res.Stderr)
		if state == "throttled" || ignore == "" || !strings.Contains(stderr, ignore) {
			return Fail(fmt.Sprintf("kopia_repository: kopia repository %s failed (rc=%d): %s", subcmd, res.RC, stderr)), nil
		}
	}

	after, err := kopiaStatus(ctx, conn, config)
	if err != nil {
		return Result{}, err
	}

	if after != before {
		return Changed(after).WithExtra("kopia_repository", after), nil
	}
	return Ok(after).WithExtra("kopia_repository", after), nil
}

// kopiaRun runs `kopia <argv...>` on the target.
func kopiaRun(ctx context.Context, conn remoteexec.Connection, argv []string) (remoteexec.Result, error) {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return runStatus(ctx, conn, "kopia "+strings.Join(quoted, " "))
}

// kopiaStatus runs `kopia repository status [--config-file=...]` and
// returns its trimmed stdout, ignoring rc — matching real
// KopiaRepository._get()'s own rc-agnostic use of this same command
// purely for before/after diffing (a disconnected repository's status
// naturally exits non-zero, which is not an error for this purpose).
func kopiaStatus(ctx context.Context, conn remoteexec.Connection, config string) (string, error) {
	argv := []string{"repository", "status"}
	if config != "" {
		argv = append(argv, "--config-file="+config)
	}
	res, err := kopiaRun(ctx, conn, argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// kopiaBackendArgv builds a backend dict's own CLI argv — see
// moduleKopiaRepository's own doc comment for the provider="server"
// quirk this reproduces.
func kopiaBackendArgv(backend map[string]any) ([]string, error) {
	provider := argString(backend, "provider", "")
	if provider == "" {
		return nil, errArg("kopia_repository: backend.provider is required")
	}
	switch provider {
	case "azure", "b2", "filesystem", "gcs", "gdrive", "rclone", "s3", "sftp", "webdav", "server":
	default:
		return nil, errArg("kopia_repository: backend.provider must be one of azure, b2, filesystem, gcs, gdrive, rclone, s3, sftp, webdav, server, got %q", provider)
	}
	if provider == "server" {
		return nil, nil
	}
	for _, req := range kopiaBackendRequired[provider] {
		if argString(backend, req, "") == "" {
			return nil, errArg("kopia_repository: backend.%s is required when backend.provider=%s", req, provider)
		}
	}
	argv := []string{provider}
	for _, f := range kopiaBackendFlags[provider] {
		v, ok := backend[f.key]
		if !ok {
			continue
		}
		s := fmt.Sprint(v)
		if s == "" {
			continue
		}
		argv = append(argv, f.flag+"="+s)
	}
	return argv, nil
}

// kopiaThrottleArgv builds `--flag value` pairs (space-separated, NOT
// --flag=value, matching real _fmt_throttle's own
// `result.extend([flag, str(v)])`) from a throttle dict.
func kopiaThrottleArgv(throttle map[string]any) []string {
	var argv []string
	for _, f := range kopiaThrottleFlags {
		v, ok := throttle[f.key]
		if !ok {
			continue
		}
		argv = append(argv, f.flag, fmt.Sprint(v))
	}
	return argv
}
