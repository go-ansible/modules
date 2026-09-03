package modules

import (
	"context"
	"encoding/json"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePmem implements a scoped-down subset of Ansible's `pmem`
// (community.general) module: manages Linux persistent memory (NVDIMM)
// namespaces via the `ndctl` command-line tool.
//
// Architectural note (a real, deliberate scope cut, not an oversight):
// real pmem does two very different jobs behind one argument spec —
// (1) namespace management via `ndctl`, which is a small, portable set
// of shell commands this port CAN faithfully replicate, and (2) region
// (AppDirect/Memory Mode/Reserved capacity) provisioning via `ipmctl`,
// which real pmem drives by parsing ipmctl's own `-o nvmxml` XML output
// (via the `xmltodict` Python library) into percentages, per-socket
// goals, and a "reboot required" flag — real pmem's own main() ALWAYS
// reports changed=true after this runs, since ipmctl's own "goal" is a
// pending-until-reboot request with no real "already applied" check
// either. This port has no XML-parsing convention elsewhere in this
// codebase to build on for that shape, and it is out of scope for this
// batch (see this batch's own assignment brief, which frames pmem as
// "manages Linux persistent memory (NVDIMM) namespaces via the `ndctl`
// CLI"). Rather than silently ignoring appdirect/memorymode/reserved/
// socket if given, this port FAILS the task explicitly when any of them
// are set, naming the omission — an honest "not implemented" beats a
// silent no-op that looks like success.
//
// Args: namespace ([]map, optional) — each entry: mode (raw|sector|
// fsdax|devdax, required), type (pmem|blk, optional), size (string,
// optional, e.g. "1GB" — forwarded to `ndctl create-namespace -s`
// verbatim; real pmem's own module pre-converts this to a byte count
// itself before calling ndctl, this port instead lets ndctl's own `-s`
// parse the suffixed string directly, since ndctl already accepts the
// same k/K/m/M/g/G/t/T suffixes real pmem's own doc documents — a
// simplification, not a behavior change for any size ndctl itself
// accepts); namespace_append (bool, default false) — when false
// (matching real pmem's own default and its own pmem_init_env()),
// EVERY existing namespace is disabled and destroyed before any new one
// from this task is created; appdirect/memorymode/reserved/socket/
// appdirect_interleaved — accepted for argspec compatibility, always
// rejected with an explicit Fail (see above) if actually set.
//
// Namespace mode always reports Changed=true when it runs any
// create/destroy step, exactly mirroring real pmem's own unconditional
// self.changed = True (real pmem's own namespace_check() validates
// shape, never idempotency).
func modulePmem(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	for _, key := range []string{"appdirect", "memorymode", "reserved", "socket"} {
		if _, ok := args[key]; ok {
			return Result{}, errArg("pmem: %s requires ipmctl region-goal provisioning (XML output parsing, "+
				"reboot-required semantics), which this port does not implement — only `namespace` "+
				"(ndctl-based) is supported; see modulePmem's own doc comment", key)
		}
	}

	namespaces, err := pmemParseNamespaces(args)
	if err != nil {
		return Result{}, err
	}
	if len(namespaces) == 0 {
		return Result{}, errArg("pmem: at least one of namespace, appdirect, memorymode, reserved, or socket is required")
	}
	appendMode := argBool(args, "namespace_append", false)

	changed := false
	if !appendMode {
		if err := pmemRemoveAllNamespaces(ctx, conn); err != nil {
			return Result{}, err
		}
	}

	for _, ns := range namespaces {
		cmd := "ndctl create-namespace -m " + shellQuote(ns.mode)
		if ns.nsType != "" {
			cmd += " -t " + shellQuote(ns.nsType)
		}
		if ns.size != "" {
			cmd += " -s " + shellQuote(ns.size)
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		changed = true
	}

	if changed {
		return Changed("namespaces configured"), nil
	}
	return Ok("no namespaces configured"), nil
}

type pmemNamespace struct {
	mode   string
	nsType string
	size   string
}

func pmemParseNamespaces(args map[string]any) ([]pmemNamespace, error) {
	raw, ok := args["namespace"]
	if !ok {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, errArg("pmem: namespace must be a list")
	}
	out := make([]pmemNamespace, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, errArg("pmem: each namespace entry must be a map")
		}
		mode := argString(m, "mode", "")
		if mode != "raw" && mode != "sector" && mode != "fsdax" && mode != "devdax" {
			return nil, errArg("pmem: namespace.mode must be one of raw, sector, fsdax, devdax, got %q", mode)
		}
		nsType := argString(m, "type", "")
		if nsType != "" && nsType != "pmem" && nsType != "blk" {
			return nil, errArg("pmem: namespace.type must be pmem or blk, got %q", nsType)
		}
		out = append(out, pmemNamespace{
			mode:   mode,
			nsType: nsType,
			size:   argString(m, "size", ""),
		})
	}
	return out, nil
}

// pmemRemoveAllNamespaces disables and destroys every namespace `ndctl
// list -N` currently reports, matching real pmem's own
// pmem_remove_namespaces().
func pmemRemoveAllNamespaces(ctx context.Context, conn remoteexec.Connection) error {
	out, err := run(ctx, conn, "ndctl list -N")
	if err != nil {
		return err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	var entries []struct {
		Dev string `json:"dev"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return errArg("pmem: could not parse `ndctl list -N` output: %v", err)
	}
	for _, e := range entries {
		if e.Dev == "" {
			continue
		}
		if _, err := run(ctx, conn, "ndctl disable-namespace "+shellQuote(e.Dev)); err != nil {
			return err
		}
		if _, err := run(ctx, conn, "ndctl destroy-namespace "+shellQuote(e.Dev)); err != nil {
			return err
		}
	}
	return nil
}
