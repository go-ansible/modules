package modules

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleOneVM implements Ansible's `one_vm` module: creates OpenNebula
// VM instances from a template and manages their power state, via the
// `onevm`/`onetemplate` CLIs (see one_common.go's own doc comment).
//
// Real one_vm is, by a wide margin, the most feature-rich module in
// this whole batch (its own ansible-doc dump runs to 750 lines): exact
// instance counting against arbitrary attribute/label criteria
// (exact_count/count_attributes/count_labels), a "#"-run NAME index
// substitution scheme, `update_attributes`' own steady-state
// USER_TEMPLATE reconciliation, `updateconf`'s own CONTEXT/OS/
// FEATURES/... sub-schema, and `disk_saveas`. Reproducing ALL of that
// through shelled-out onevm/onetemplate CLI calls, with no live
// OpenNebula cluster in this sandbox to verify exact XML output shapes
// against, is beyond what this port can honestly claim to replicate —
// per this batch's own instructions ("if real behavior can't be
// replicated through this port's architecture, document that honestly
// rather than faking it", the same stance packet_volume.go's own gap
// takes). So this port implements the common, verifiable core exactly
// (create N instances from a template; target existing instances by
// instance_ids or by an EXACT name/label match; running/poweredoff/
// rebooted/absent state transitions; owner/group/mode at creation) and
// FAILS LOUD — Result{Failed:true}, not a silent no-op — for the
// arguments it does not implement, rather than silently ignoring them:
//
//	exact_count, count_attributes, count_labels, update_attributes,
//	updateconf, disk_saveas
//
// # Implemented behavior
//
// Args: state (present|absent|running|rebooted|poweredoff, default
// "present"); template_id / template_name (create source); count (int,
// default 1); instance_ids ([]int, aliased ids) — targets for
// absent/running/rebooted/poweredoff; attributes (dict) — NAME (case-
// insensitive key) sets the new VM's name (a "#"-run in NAME is
// replaced with a zero-padded creation index, matching real one_vm's
// own convention, e.g. "foo-###" -> "foo-000", "foo-001", ...; ALSO
// used, when instance_ids is empty, as an EXACT-match selector for
// running/rebooted/poweredoff/absent — NOT the "#"-wildcard pattern
// matching real one_vm's own examples show for that use ("name:
// fooapp-#" matching every "fooapp-NN" instance): this port only
// matches an exact, literal NAME, a real narrowing, not a guess);
// labels ([]string) — set as the new VM's own LABELS (comma-joined),
// and, like attributes, usable as a selector (a VM's own LABELS must
// contain every requested label) when instance_ids is empty; memory/
// vcpu/cpu/disk_size — passed straight through as extra template
// overrides at instantiation (a single disk_size value only — the
// multi-disk-in-order behavior real one_vm documents for a multi-disk
// template is not implemented); group_id/owner_id/mode — applied via
// `onevm chgrp`/`chown`/`chmod` right after creation; persistent (bool)
// -> `--persistent`; vm_start_on_hold (bool) -> `--hold`; hard (bool)
// -> `--hard` on terminate/poweroff/reboot. wait/wait_timeout are
// accepted but NOT polled for (same documented-gap stance as
// one_host.go's own wait note).
//
// Facts (Extra "instances"/"instances_ids") report each affected VM's
// own id, name, state, lcm_state (both the RAW numeric text from
// `onevm show -x` — this port does not map either to real one_vm's own
// VM_STATES/LCM_STATE name tables, a bounded, honestly-declared
// narrowing rather than a guessed enum), cpu, vcpu, memory, disk_size,
// networks (from each NIC), attributes (the VM's own USER_TEMPLATE, as
// a structural map), labels, group_id/group_name, owner_id/owner_name,
// mode, template_id. "tagged_instances" (real one_vm's own
// count_attributes/count_labels-driven return value) is never
// populated, matching the exact_count gap above.
func moduleOneVM(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	for _, unimplemented := range []string{"exact_count", "count_attributes", "count_labels", "update_attributes", "updateconf", "disk_saveas"} {
		if v, ok := args[unimplemented]; ok && !oneIsEmptyArg(v) {
			return Fail(fmt.Sprintf("one_vm: %s is not implemented by this port (a live OpenNebula cluster was "+
				"not available to verify its exact XML/CLI shape) — see moduleOneVM's own doc comment", unimplemented)), nil
		}
	}
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent", "running", "rebooted", "poweredoff":
	default:
		return Result{}, errArg("one_vm: state must be one of present, absent, running, rebooted, poweredoff, got %q", state)
	}
	url := oneAuth(args)
	if res, ok := oneRequireBinary(ctx, conn, "onevm", "one_vm"); !ok {
		return res, nil
	}

	if state == "present" {
		return oneVMCreate(ctx, conn, url, args)
	}
	return oneVMTransition(ctx, conn, url, state, args)
}

func oneIsEmptyArg(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	default:
		return false
	}
}

func oneVMCreate(ctx context.Context, conn remoteexec.Connection, url string, args map[string]any) (Result, error) {
	if _, ok := args["template_id"]; !ok {
		if argString(args, "template_name", "") == "" {
			return Result{}, errArg("one_vm: template_id or template_name is required when state is present")
		}
	}
	if res, ok := oneRequireBinary(ctx, conn, "onetemplate", "one_vm"); !ok {
		return res, nil
	}

	var templateID string
	if v, ok := args["template_id"]; ok && v != nil {
		templateID = fmtAny(v)
	} else {
		pool, err := oneListXML(ctx, conn, url, "onetemplate")
		if err != nil {
			return Result{}, err
		}
		item, ok := oneResolveByName(pool, "VMTEMPLATE", argString(args, "template_name", ""))
		if !ok {
			return Fail("one_vm: no template with name " + argString(args, "template_name", "")), nil
		}
		templateID = item.childText("ID")
	}

	count := argInt(args, "count", 1)
	attrs, _ := args["attributes"].(map[string]any)
	name := oneCaseInsensitiveLookup(attrs, "name")
	labels := argStringList(args, "labels")
	onHold := argBool(args, "vm_start_on_hold", false)
	persistent := argBool(args, "persistent", false)
	groupID, hasGroup := args["group_id"]
	ownerID, hasOwner := args["owner_id"]
	mode := argString(args, "mode", "")

	extra := map[string]any{}
	for k, v := range attrs {
		if strings.EqualFold(k, "name") {
			continue
		}
		extra[strings.ToUpper(k)] = v
	}
	if len(labels) > 0 {
		extra["LABELS"] = labels
	}
	if v, ok := args["memory"]; ok {
		extra["MEMORY"] = v
	}
	if v, ok := args["vcpu"]; ok {
		extra["VCPU"] = v
	}
	if v, ok := args["cpu"]; ok {
		extra["CPU"] = v
	}
	if ds := argStringList(args, "disk_size"); len(ds) == 1 {
		extra["SIZE"] = ds[0]
	}

	instances := []any{}
	ids := []any{}
	for i := 0; i < count; i++ {
		vmName := oneVMDerivedName(name, i)
		argv := []string{"instantiate", templateID}
		if vmName != "" {
			argv = append(argv, "--name", vmName)
		}
		if onHold {
			argv = append(argv, "--hold")
		}
		if persistent {
			argv = append(argv, "--persistent")
		}
		body := oneRenderTemplate(extra)
		var res remoteexec.Result
		var err error
		if body != "" {
			argv = append(argv, "-")
			res, err = oneRunStdin(ctx, conn, url, "onetemplate", body, argv...)
		} else {
			res, err = oneRun(ctx, conn, url, "onetemplate", argv...)
		}
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("one_vm: instantiating template: " + oneErrMsg(res)), nil
		}
		m := oneVMIDRe.FindStringSubmatch(res.Stdout)
		if m == nil {
			return Fail("one_vm: could not parse the new VM's own ID from `onetemplate instantiate`'s own stdout: " + strings.TrimSpace(res.Stdout)), nil
		}
		vmID := m[1]

		if hasOwner || hasGroup {
			ownerArg := "0"
			if hasOwner {
				ownerArg = fmtAny(ownerID)
			}
			chownArgv := []string{"chown", vmID, ownerArg}
			if hasGroup {
				chownArgv = append(chownArgv, fmtAny(groupID))
			}
			if res, err := oneRun(ctx, conn, url, "onevm", chownArgv...); err != nil {
				return Result{}, err
			} else if res.RC != 0 {
				return Fail("one_vm: chown: " + oneErrMsg(res)), nil
			}
		}
		if mode != "" {
			if res, err := oneRun(ctx, conn, url, "onevm", "chmod", vmID, mode); err != nil {
				return Result{}, err
			} else if res.RC != 0 {
				return Fail("one_vm: chmod: " + oneErrMsg(res)), nil
			}
		}

		vm, found, err := oneShowXML(ctx, conn, url, "onevm", vmID)
		if err != nil {
			return Result{}, err
		}
		if !found {
			return Fail("one_vm: VM " + vmID + " was created but could not be found afterwards"), nil
		}
		instances = append(instances, oneVMFacts(vm))
		n, _ := strconv.Atoi(vmID)
		ids = append(ids, n)
	}

	res := Result{Changed: count > 0}
	res = res.WithExtra("instances", instances).WithExtra("instances_ids", ids)
	return res, nil
}

var oneVMIDRe = regexp.MustCompile(`VM ID:\s*(\d+)`)

// oneVMDerivedName replaces a run of "#" characters in name with a
// zero-padded creation index (e.g. "foo-###" -> "foo-000" for i=0),
// matching real one_vm's own NAME convention; a name with no "#" is
// returned unchanged for every instance (a real ambiguity for count>1
// this port does not resolve any more cleverly than real one_vm's own
// underlying pyone allocate call would).
func oneVMDerivedName(name string, i int) string {
	if name == "" {
		return ""
	}
	idx := strings.IndexRune(name, '#')
	if idx < 0 {
		return name
	}
	end := idx
	for end < len(name) && name[end] == '#' {
		end++
	}
	width := end - idx
	digits := strconv.Itoa(i)
	if len(digits) < width {
		digits = strings.Repeat("0", width-len(digits)) + digits
	}
	return name[:idx] + digits + name[end:]
}

func oneCaseInsensitiveLookup(m map[string]any, key string) string {
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return fmtAny(v)
		}
	}
	return ""
}

func oneVMTransition(ctx context.Context, conn remoteexec.Connection, url, state string, args map[string]any) (Result, error) {
	ids := oneVMTargetIDs(args)
	var targets []string
	if len(ids) > 0 {
		targets = ids
	} else {
		attrs, _ := args["attributes"].(map[string]any)
		name := oneCaseInsensitiveLookup(attrs, "name")
		labels := argStringList(args, "labels")
		if name == "" && len(labels) == 0 {
			res := Result{Changed: false}
			return res.WithExtra("instances", []any{}).WithExtra("instances_ids", []any{}), nil
		}
		pool, err := oneListXML(ctx, conn, url, "onevm")
		if err != nil {
			return Result{}, err
		}
		for _, vm := range pool.children("VM") {
			if name != "" && vm.childText("NAME") != name {
				continue
			}
			if len(labels) > 0 && !oneVMHasLabels(vm, labels) {
				continue
			}
			targets = append(targets, vm.childText("ID"))
		}
	}

	hard := argBool(args, "hard", false)
	var verb string
	switch state {
	case "absent":
		verb = "terminate"
	case "running":
		verb = "resume"
	case "poweredoff":
		verb = "poweroff"
	case "rebooted":
		verb = "reboot"
	}

	instances := []any{}
	instIDs := []any{}
	changed := false
	for _, id := range targets {
		argv := []string{verb, id}
		if hard && verb != "resume" {
			argv = append(argv, "--hard")
		}
		res, err := oneRun(ctx, conn, url, "onevm", argv...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(fmt.Sprintf("one_vm: %s %s: %s", verb, id, oneErrMsg(res))), nil
		}
		changed = true
		n, _ := strconv.Atoi(id)
		instIDs = append(instIDs, n)
		if state != "absent" {
			vm, found, err := oneShowXML(ctx, conn, url, "onevm", id)
			if err != nil {
				return Result{}, err
			}
			if found {
				instances = append(instances, oneVMFacts(vm))
			}
		}
	}

	res := Result{Changed: changed}
	res = res.WithExtra("instances", instances).WithExtra("instances_ids", instIDs)
	return res, nil
}

func oneVMTargetIDs(args map[string]any) []string {
	list := argStringList(args, "instance_ids")
	if list == nil {
		list = argStringList(args, "ids")
	}
	return list
}

func oneVMHasLabels(vm oneXMLNode, want []string) bool {
	have := map[string]bool{}
	var labelsText string
	if userTemplate, ok := vm.child("USER_TEMPLATE"); ok {
		labelsText = userTemplate.childText("LABELS")
	}
	if labelsText == "" {
		labelsText = vm.childText("LABELS")
	}
	for _, l := range strings.Split(labelsText, ",") {
		have[strings.TrimSpace(l)] = true
	}
	for _, w := range want {
		if !have[w] {
			return false
		}
	}
	return true
}

func oneVMFacts(vm oneXMLNode) map[string]any {
	facts := map[string]any{
		"vm_id":       vm.childInt("ID"),
		"vm_name":     vm.childText("NAME"),
		"state":       vm.childText("STATE"),
		"lcm_state":   vm.childText("LCM_STATE"),
		"owner_id":    vm.childInt("UID"),
		"owner_name":  vm.childText("UNAME"),
		"group_id":    vm.childInt("GID"),
		"group_name":  vm.childText("GNAME"),
		"template_id": "",
	}
	if tmpl, ok := vm.child("TEMPLATE"); ok {
		facts["cpu"] = tmpl.childText("CPU")
		facts["vcpu"] = tmpl.childText("VCPU")
		facts["memory"] = tmpl.childText("MEMORY")
		facts["template_id"] = tmpl.childText("TEMPLATE_ID")
		var networks []any
		for _, nic := range tmpl.children("NIC") {
			networks = append(networks, map[string]any{
				"ip": nic.childText("IP"), "mac": nic.childText("MAC"),
				"name": nic.childText("NETWORK"), "security_groups": nic.childText("SECURITY_GROUPS"),
			})
		}
		if networks == nil {
			networks = []any{}
		}
		facts["networks"] = networks
		var diskSize string
		if disk, ok := tmpl.child("DISK"); ok {
			diskSize = disk.childText("SIZE")
		}
		facts["disk_size"] = diskSize
	}
	if perms, ok := vm.child("PERMISSIONS"); ok {
		octal := func(u, m, a string) string {
			ui, _ := strconv.Atoi(perms.childText(u))
			mi, _ := strconv.Atoi(perms.childText(m))
			ai, _ := strconv.Atoi(perms.childText(a))
			return strconv.Itoa(ui*4 + mi*2 + ai)
		}
		facts["mode"] = octal("OWNER_U", "OWNER_M", "OWNER_A") + octal("GROUP_U", "GROUP_M", "GROUP_A") + octal("OTHER_U", "OTHER_M", "OTHER_A")
	}
	if ut, ok := vm.child("USER_TEMPLATE"); ok {
		facts["attributes"] = ut.toMap()
		facts["labels"] = oneSplitLabels(ut.childText("LABELS"))
	} else {
		facts["attributes"] = map[string]any{}
		facts["labels"] = []any{}
	}
	return facts
}

func oneSplitLabels(s string) []any {
	if strings.TrimSpace(s) == "" {
		return []any{}
	}
	parts := strings.Split(s, ",")
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}
