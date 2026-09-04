package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSlVm implements (a subset of) Ansible's `sl_vm`
// (community.general) module: creates or cancels an IBM SoftLayer /
// IBM Cloud Classic Infrastructure virtual server, via IBM's own
// official CLI, `slcli` (github.com/softlayer/softlayer-python, the
// same package real sl_vm.py's own `SoftLayer`/`SoftLayer.VSManager`
// Python client ships in — `slcli` is that package's bundled CLI
// entry point) — the same "shell out to the platform's own official
// CLI instead of an API client" precedent this port already applies
// to Consul, Redis, Terraform, Icinga2, Kopia, GitHub, GitLab,
// Keycloak, Scaleway, and Alibaba Cloud in this batch. `slcli`'s own
// `vs create`/`vs list`/`vs cancel` flags used below are verified
// against softlayer-python's own CLI source (SoftLayer/CLI/virt/
// create.py, list.py, cancel.py) fetched directly from its GitHub
// repository — not guessed from the module name.
//
// # Auth precondition
//
// `slcli` must already be configured on the TARGET host before this
// module runs: either a prior `slcli setup` (which writes
// ~/.softlayer, slcli's own credentials file, containing the SoftLayer
// API username+key) has already run there, or the SL_USERNAME/SL_API_KEY
// environment variables are already exported in that session's own
// environment — the same shape of precondition ali_common.go's own doc
// comment sets for `aliyun configure`. This port does not attempt to
// drive `slcli setup` (an interactive credential-entry ceremony)
// itself. Real sl_vm.py's own credentials come from
// SoftLayer.create_client_from_env() (the SAME env vars/config file
// slcli itself reads — softlayer-python is one package backing both),
// so there is no separate auth argument on real sl_vm to wire through
// at all; this port matches that exactly.
//
// # `--format json`
//
// Every slcli invocation below passes the global `--format json` flag
// (confirmed from softlayer-python's own docs: `--format`
// [table|raw|json|jsonraw], default raw) so this port can decode
// structured output. slcli's own JSON shape for `vs list`/`vs create`
// is NOT pinned down by softlayer-python's own generated CLI reference
// docs (which describe flags, not exact JSON field names) and this
// port has no live `slcli` binary/SoftLayer account in this sandbox to
// verify a real response against — this port therefore decodes
// defensively (see slDecodeID below), the same honesty gitlab_common.go's
// own doc comment already applies to `glab api`'s flag surface, not
// verified against a live binary.
//
// Args: instance_id; hostname; domain; datacenter (choices, see real
// sl_vm's own DATACENTERS list); tags (string OR list of strings — real
// sl_vm's own argspec declares `type: str` but its own EXAMPLES pass a
// YAML list; this port accepts either, matching real create_virtual_instance's
// own `",".join(...)` list-to-string folding, then splits on "," and
// sends each as its own `--tag`); hourly (bool, default true) — sent as
// `--billing hourly`/`--billing monthly`; private (bool, default
// false); dedicated (bool, default false); local_disk (bool, default
// true) — false adds `--san`; cpus (int); memory (int); flavor
// (string) — mutually exclusive with cpus/memory on real slcli, this
// port does not enforce that itself; disks ([]int, default [25]); os_code
// or image_id — mutually exclusive, matching real create_virtual_instance's
// own branching (os_code, if set, wins; if NEITHER is set, this port
// matches real sl_vm's own silent no-op: Changed=false, no create
// attempted, no error); nic_speed (int); public_vlan/private_vlan
// (string); ssh_keys ([]string); post_uri (string); state
// (present|absent, default present); wait (bool, default true) —
// sent as slcli's own native `--wait <wait_time>` (a real provisioning-
// completion wait built into `vs create` itself, simpler and more
// faithful than polling a separate readiness command); wait_time (int,
// default 600).
//
// # Idempotency (state=present)
//
// `slcli vs list --hostname H --domain D --datacenter DC` first; if
// ANY match is found, this port returns Changed=false with that first
// match as Extra["instance"] — matching real create_virtual_instance's
// own list_instances(...) short-circuit exactly (no comparison of
// cpu/memory/os_code/... against the existing instance, matching real
// sl_vm's own behavior: existence alone is enough).
//
// # state=absent — a faithful, deliberately-preserved real quirk
//
// Real sl_vm.py's own cancel_instance() catches every exception from
// vsManager.cancel_instance() and sets canceled=False WITHOUT ever
// calling fail_json — a cancel that errors (e.g. a nonexistent
// instance_id) is reported as Changed=false, not a task failure. This
// port matches that exactly: a nonzero `slcli vs cancel` RC is treated
// as Changed=false (Ok, not Fail) — a deliberately-preserved real
// behavior, not a bug in this port. When instance_id is unset and
// tags/hostname/domain are given, every matching instance (via `slcli
// vs list`) is canceled the same way; when none of instance_id/tags/
// hostname/domain are given, this is a silent no-op (Changed=false),
// matching real cancel_instance's own final `else: return False, None`.
//
// Deviation from real sl_vm.py: its own cancel_instance() elif branch
// (instance_id given) references the local variable `instance`, which
// is never assigned anywhere in that function — a real NameError bug
// in real sl_vm.py that would crash Python outright rather than cancel
// anything by ID. This port implements the clearly INTENDED behavior
// (cancel BY instance_id) instead of reproducing a crash, the same
// judgment call aix_devices.go's own doc comment already makes for an
// analogous real upstream defect.
func moduleSlVm(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "sl_vm"
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("%s: state must be one of present, absent, got %q", mod, state)
	}
	if res, ok := slRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}

	if state == "absent" {
		return slVmCancel(ctx, conn, args)
	}
	return slVmCreate(ctx, conn, args)
}

func slRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v slcli"); err != nil {
		return Fail(fmt.Sprintf("%s: the slcli binary (IBM SoftLayer / IBM Cloud Classic Infrastructure's own "+
			"CLI, from the softlayer-python package) is required on the target and was not found in PATH — "+
			"this port shells out to it rather than speaking the SoftLayer API via the SoftLayer Python client "+
			"directly; see moduleSlVm's own doc comment, including the precondition that `slcli setup` must "+
			"already have been run (or SL_USERNAME/SL_API_KEY already set) on the target", moduleName)), false
	}
	return Result{}, true
}

func slCmd(argv ...string) string {
	full := append([]string{"slcli", "--format", "json"}, argv...)
	quoted := make([]string, len(full))
	for i, a := range full {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

func slRun(ctx context.Context, conn remoteexec.Connection, argv ...string) (remoteexec.Result, error) {
	return runStatus(ctx, conn, slCmd(argv...))
}

func slErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}

// slListInstances runs `slcli vs list <filters...>` and decodes its
// JSON array of instances (defensively — see moduleSlVm's own doc
// comment on why the exact field set isn't pinned down).
func slListInstances(ctx context.Context, conn remoteexec.Connection, filters ...string) ([]map[string]any, remoteexec.Result, error) {
	argv := append([]string{"vs", "list"}, filters...)
	res, err := slRun(ctx, conn, argv...)
	if err != nil {
		return nil, res, err
	}
	if res.RC != 0 {
		return nil, res, nil
	}
	var items []map[string]any
	if s := strings.TrimSpace(res.Stdout); s != "" {
		if jerr := json.Unmarshal([]byte(s), &items); jerr != nil {
			return nil, res, fmt.Errorf("decoding slcli vs list output: %w", jerr)
		}
	}
	return items, res, nil
}

// slInstanceID extracts an instance's own ID from a decoded slcli JSON
// record, trying both "id" and "Id" (see moduleSlVm's own doc comment
// on why this port can't pin the exact key name down without a live
// slcli binary).
func slInstanceID(item map[string]any) string {
	if v, ok := item["id"]; ok {
		return fmt.Sprint(v)
	}
	if v, ok := item["Id"]; ok {
		return fmt.Sprint(v)
	}
	return ""
}

func slVmCreate(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "sl_vm"
	hostname := argString(args, "hostname", "")
	domain := argString(args, "domain", "")
	datacenter := argString(args, "datacenter", "")

	var listFilters []string
	if hostname != "" {
		listFilters = append(listFilters, "--hostname", hostname)
	}
	if domain != "" {
		listFilters = append(listFilters, "--domain", domain)
	}
	if datacenter != "" {
		listFilters = append(listFilters, "--datacenter", datacenter)
	}
	existing, res, err := slListInstances(ctx, conn, listFilters...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("%s: failed to list existing instances: %s", mod, slErrMsg(res))), nil
	}
	if len(existing) > 0 {
		return Ok("").WithExtra("instance", existing[0]), nil
	}

	osCode := argString(args, "os_code", "")
	imageID := argString(args, "image_id", "")
	if osCode == "" && imageID == "" {
		// Matches real create_virtual_instance's own silent no-op when
		// neither is given — see moduleSlVm's own doc comment.
		return Ok(mod + ": neither os_code nor image_id given, nothing to create"), nil
	}

	argv := []string{"vs", "create", "--hostname", hostname, "--domain", domain}
	if datacenter != "" {
		argv = append(argv, "--datacenter", datacenter)
	}
	if flavor := argString(args, "flavor", ""); flavor != "" {
		argv = append(argv, "--flavor", flavor)
	} else {
		if _, ok := args["cpus"]; ok {
			argv = append(argv, "--cpu", strconv.Itoa(argInt(args, "cpus", 0)))
		}
		if _, ok := args["memory"]; ok {
			argv = append(argv, "--memory", strconv.Itoa(argInt(args, "memory", 0)))
		}
	}
	if argBool(args, "hourly", true) {
		argv = append(argv, "--billing", "hourly")
	} else {
		argv = append(argv, "--billing", "monthly")
	}
	if argBool(args, "dedicated", false) {
		argv = append(argv, "--dedicated")
	}
	if argBool(args, "private", false) {
		argv = append(argv, "--private")
	}
	if !argBool(args, "local_disk", true) {
		argv = append(argv, "--san")
	}
	if osCode != "" {
		argv = append(argv, "--os", osCode)
		for _, d := range slIntList(args, "disks") {
			argv = append(argv, "--disk", strconv.Itoa(d))
		}
	} else {
		argv = append(argv, "--image", imageID)
	}
	if v, ok := args["nic_speed"]; ok {
		argv = append(argv, "--network", fmt.Sprint(v))
	}
	if v := argString(args, "public_vlan", ""); v != "" {
		argv = append(argv, "--vlan-public", v)
	}
	if v := argString(args, "private_vlan", ""); v != "" {
		argv = append(argv, "--vlan-private", v)
	}
	for _, k := range argStringList(args, "ssh_keys") {
		argv = append(argv, "--key", k)
	}
	if v := argString(args, "post_uri", ""); v != "" {
		argv = append(argv, "--postinstall", v)
	}
	for _, tag := range slTagList(args) {
		argv = append(argv, "--tag", tag)
	}
	if argBool(args, "wait", true) {
		argv = append(argv, "--wait", strconv.Itoa(argInt(args, "wait_time", 600)))
	}

	createRes, err := slRun(ctx, conn, argv...)
	if err != nil {
		return Result{}, err
	}
	if createRes.RC != 0 {
		return Fail(fmt.Sprintf("%s: failed to create instance: %s", mod, slErrMsg(createRes))), nil
	}
	var created map[string]any
	if s := strings.TrimSpace(createRes.Stdout); s != "" {
		_ = json.Unmarshal([]byte(s), &created)
	}
	return Changed("").WithExtra("instance", created), nil
}

func slVmCancel(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	instanceID := argString(args, "instance_id", "")
	hostname := argString(args, "hostname", "")
	domain := argString(args, "domain", "")
	tags := slTagList(args)

	if instanceID != "" {
		res, err := slRun(ctx, conn, "-y", "vs", "cancel", instanceID)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			// Matches real cancel_instance's own swallowed-exception
			// behavior — see moduleSlVm's own doc comment.
			return Ok(""), nil
		}
		return Changed(""), nil
	}

	if hostname == "" && domain == "" && len(tags) == 0 {
		return Ok(""), nil
	}

	var listFilters []string
	for _, t := range tags {
		listFilters = append(listFilters, "--tag", t)
	}
	if hostname != "" {
		listFilters = append(listFilters, "--hostname", hostname)
	}
	if domain != "" {
		listFilters = append(listFilters, "--domain", domain)
	}
	instances, res, err := slListInstances(ctx, conn, listFilters...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("sl_vm: failed to list instances to cancel: %s", slErrMsg(res))), nil
	}

	canceled := true
	for _, inst := range instances {
		id := slInstanceID(inst)
		if id == "" {
			continue
		}
		r, err := slRun(ctx, conn, "-y", "vs", "cancel", id)
		if err != nil {
			return Result{}, err
		}
		if r.RC != 0 {
			canceled = false
		}
	}
	return Result{Changed: canceled && len(instances) > 0}, nil
}

// slIntList reads args[key] as a []int (Ansible `type: list, elements:
// int`).
func slIntList(args map[string]any, key string) []int {
	v, ok := args[key]
	if !ok {
		return nil
	}
	var raw []any
	switch x := v.(type) {
	case []any:
		raw = x
	case []int:
		out := make([]int, len(x))
		copy(out, x)
		return out
	default:
		return nil
	}
	out := make([]int, 0, len(raw))
	for _, e := range raw {
		switch n := e.(type) {
		case int:
			out = append(out, n)
		case float64:
			out = append(out, int(n))
		case string:
			if p, err := strconv.Atoi(n); err == nil {
				out = append(out, p)
			}
		}
	}
	return out
}

// slTagList reads args["tags"] as a []string, accepting either a plain
// (optionally comma-separated) string or a list — see moduleSlVm's own
// doc comment on why both shapes are accepted.
func slTagList(args map[string]any) []string {
	v, ok := args["tags"]
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case string:
		if x == "" {
			return nil
		}
		parts := strings.Split(x, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			out = append(out, fmt.Sprint(e))
		}
		return out
	case []string:
		return x
	}
	return nil
}
