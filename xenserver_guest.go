package modules

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleXenserverGuest implements Ansible's `xenserver_guest`
// (community.general) module: creates, reconfigures, and removes a
// XenServer VM. See xeBin's own doc comment (xenserver_common.go) for
// this port's `xe` CLI substitution — real xenserver_guest.py is
// XenAPI's single largest module (over 2000 lines), and this port does
// NOT attempt a byte-exact reproduction of its full reconfiguration
// state machine; every deviation below is a deliberate, documented
// narrowing, not an oversight.
//
// Args: hostname/username/password/validate_certs — see xeConnArgs's
// own doc comment; name (aliases name_label) or uuid — uuid alone
// cannot create a VM (matching real xenserver_guest.py's own "a
// supplied UUID is ignored on VM creation" note: XenServer always
// mints its own UUID), so creating a not-yet-existing VM requires name;
// state (present|absent|poweredon, default "present"); template
// (aliases template_src) / template_uuid — the source template/VM/
// snapshot to clone from when creating; linked_clone (bool, default
// false) — `xe vm-clone` (fast, storage-level clone) when true, `xe
// vm-copy` (full copy) when false, matching real xenserver_guest.py's
// own linked_clone semantics; name_desc; folder — written to
// other-config:folder; home_server — resolved to a host UUID and
// written to the VM's own affinity; is_template (bool); force (bool) —
// required to reconfigure hardware/disk size on a VM that isn't
// currently shut down, and to remove a VM that is still running (both
// matching real xenserver_guest.py's own documented use of force);
// hardware.memory_mb/num_cpus/num_cpu_cores_per_socket; disks (aliases
// disk, list of {name/name_label, name_desc, size or one of
// size_[tb,gb,mb,kb,b], sr, sr_uuid}) — NEW disks only: matching real
// xenserver_guest.py's own documented "Removing or detaching existing
// disks of VM is not supported", this port narrows further and also
// does not resize an already-existing disk (real xenserver_guest.py
// does, when the VM is shut down); cdrom ({type: none|iso, iso_name}) —
// only reconfigures an ALREADY-EXISTING CD-ROM VBD (present on most
// templates); a template with no CD-ROM device at all is left without
// one, a narrowing from real xenserver_guest.py which creates one on
// demand; networks (aliases network, list of {name/name_label, mac,
// type, ip, netmask, gateway, type6, ip6, gateway6}) — NEW VIFs only,
// same disks-style narrowing; ip/netmask/gateway/ip6/gateway6 are
// always written to the VM's own xenstore-data
// (vm-data/networks/<device>/<field>) — this port's own "custom"
// customization_agent path (see xeVMFacts's own doc comment) — never
// XenServer's native in-guest-agent configuration path real
// xenserver_guest.py prefers on XenServer >= 7.0; custom_params (list
// of {key, value}) — each applied via `xe vm-param-set uuid=<uuid>
// <key>=<value>`, matching real xenserver_guest.py's own documented
// purpose ("advanced users familiar with managing VM params through xe
// CLI") almost exactly, since this port already goes through that same
// CLI; wait_for_ip_address / state_change_timeout — see xeWaitForIP's
// own doc comment.
//
// State semantics: state=absent removes the VM (hard-shutdown first if
// running and force=true, else Result{Failed:true} if still running —
// matching real xenserver_guest.py's own force-gated removal of a
// running VM) via `xe vm-uninstall --force` (also removing its own
// disks, matching the doc's own "removed with associated components").
// state=present/poweredon: create from template if the VM doesn't
// exist yet (Result{Failed:true} if neither template nor template_uuid
// resolves to exactly one object), then apply name_desc/folder/
// home_server/is_template/hardware/new disks/cdrom/new networks/
// custom_params — hardware/disk-size changes are skipped with
// Result{Failed:true} when the VM is running and force is not set,
// matching real xenserver_guest.py's own "VM needs to be shut down"
// requirement; state=poweredon additionally powers the VM on
// (idempotent, via xeSetPowerState) after every other change is
// applied.
//
// Extra["changes"]: an ordered []string of the high-level steps this
// port actually took — a coarser substitute for real xenserver_guest.py's
// own structured per-field "changes" list, documented here rather than
// silently reshaped. Extra["instance"]: the VM's post-change fact tree
// (see xeVMFacts), always populated.
func moduleXenserverGuest(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "name", argString(args, "name_label", ""))
	uuidArg := argString(args, "uuid", "")
	if name == "" && uuidArg == "" {
		return Result{}, errArg("xenserver_guest: one of name (or its alias name_label) or uuid is required")
	}
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent", "poweredon":
	default:
		return Result{}, errArg("xenserver_guest: state must be present, absent, or poweredon, got %q", state)
	}
	force := argBool(args, "force", false)

	vmUUID, existed, failMsg, err := xeGuestResolve(ctx, conn, args, name, uuidArg)
	if err != nil {
		return Result{}, err
	}
	if failMsg != "" {
		return Fail(failMsg), nil
	}

	if state == "absent" {
		if !existed {
			return Ok(name + " already absent"), nil
		}
		powerState, _ := run(ctx, conn, xeCmdLine(args, []string{"vm-param-get", "uuid=" + vmUUID, "param-name=power-state"}))
		if xeModuleVMState(powerState) != "poweredoff" {
			if !force {
				return Fail(fmt.Sprintf("Cannot remove VM %s in state '%s' without force=true", name, xeModuleVMState(powerState))), nil
			}
			if res, err := xeRun(ctx, conn, args, []string{"vm-shutdown", "--force", "uuid=" + vmUUID}); err != nil {
				return Result{}, err
			} else if res.RC != 0 {
				return Fail("xenserver_guest: shutting down " + name + ": " + strings.TrimSpace(res.Stderr)), nil
			}
		}
		res, err := xeRun(ctx, conn, args, []string{"vm-uninstall", "uuid=" + vmUUID, "--force"})
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("xenserver_guest: removing " + name + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed(name+" removed").WithExtra("changes", []string{"destroy"}), nil
	}

	var changes []string

	if !existed {
		newUUID, cloneFailMsg, err := xeGuestCreate(ctx, conn, args, name)
		if err != nil {
			return Result{}, err
		}
		if cloneFailMsg != "" {
			return Fail(cloneFailMsg), nil
		}
		vmUUID = newUUID
		changes = append(changes, "create")
	}

	powerState, err := run(ctx, conn, xeCmdLine(args, []string{"vm-param-get", "uuid=" + vmUUID, "param-name=power-state"}))
	if err != nil {
		return Result{}, err
	}
	shutOff := xeModuleVMState(powerState) == "poweredoff"

	if v := argString(args, "name_desc", ""); v != "" {
		if err := xeParamSet(ctx, conn, args, vmUUID, map[string]string{"name-description": v}); err != nil {
			return Result{}, err
		}
		changes = append(changes, "name_desc")
	}
	if v := argString(args, "folder", ""); v != "" {
		if err := xeParamSet(ctx, conn, args, vmUUID, map[string]string{"other-config:folder": v}); err != nil {
			return Result{}, err
		}
		changes = append(changes, "folder")
	}
	if v := argString(args, "home_server", ""); v != "" {
		hostUUID, err := xeResolveHostUUID(ctx, conn, args, v)
		if err != nil {
			return Result{}, err
		}
		if hostUUID != "" {
			if err := xeParamSet(ctx, conn, args, vmUUID, map[string]string{"affinity": hostUUID}); err != nil {
				return Result{}, err
			}
			changes = append(changes, "home_server")
		}
	}
	if _, ok := args["is_template"]; ok {
		if err := xeParamSet(ctx, conn, args, vmUUID, map[string]string{"is-a-template": strconv.FormatBool(argBool(args, "is_template", false))}); err != nil {
			return Result{}, err
		}
		changes = append(changes, "is_template")
	}

	if hw := argMapAny(args, "hardware"); len(hw) > 0 {
		if !shutOff && !force {
			return Fail(fmt.Sprintf("xenserver_guest: VM %s must be shut down to reconfigure hardware (or set force=true)", name)), nil
		}
		if err := xeGuestApplyHardware(ctx, conn, args, vmUUID, hw); err != nil {
			return Result{}, err
		}
		changes = append(changes, "hardware")
	}

	if newDisks, err := xeGuestAddDisks(ctx, conn, args, vmUUID, shutOff, force); err != nil {
		return Result{}, err
	} else if newDisks {
		changes = append(changes, "disks_new")
	}

	if cdChanged, err := xeGuestApplyCDROM(ctx, conn, args, vmUUID); err != nil {
		return Result{}, err
	} else if cdChanged {
		changes = append(changes, "cdrom")
	}

	if newNets, err := xeGuestAddNetworks(ctx, conn, args, vmUUID); err != nil {
		return Result{}, err
	} else if newNets {
		changes = append(changes, "networks_new")
	}

	if cps := argAnyList(args, "custom_params"); len(cps) > 0 {
		for _, cp := range cps {
			m, ok := cp.(map[string]any)
			if !ok {
				continue
			}
			key := fmt.Sprint(m["key"])
			val := fmt.Sprint(m["value"])
			if key == "" {
				continue
			}
			if err := xeParamSet(ctx, conn, args, vmUUID, map[string]string{key: val}); err != nil {
				return Result{}, err
			}
		}
		changes = append(changes, "custom_params")
	}

	if state == "poweredon" {
		if c, _, transitionFailMsg, err := xeSetPowerState(ctx, conn, args, vmUUID, "poweredon", argInt(args, "state_change_timeout", 0)); err != nil {
			return Result{}, err
		} else if transitionFailMsg != "" {
			return Fail(transitionFailMsg), nil
		} else if c {
			changes = append(changes, "poweredon")
		}
	}

	if argBool(args, "wait_for_ip_address", false) {
		if _, err := xeWaitForIP(ctx, conn, args, vmUUID, argInt(args, "state_change_timeout", 0)); err != nil {
			return Result{}, err
		}
	}

	facts, err := xeVMFacts(ctx, conn, args, vmUUID)
	if err != nil {
		return Result{}, err
	}
	res := Result{Changed: len(changes) > 0}
	res = res.WithExtra("changes", changes)
	res = res.WithExtra("instance", facts)
	return res, nil
}

// xeGuestResolve resolves name/uuidArg to (at most one) existing VM,
// returning existed=false (with no failMsg) when name was given, uuid
// was not, and no VM matches — the "does not exist yet, safe to create"
// case real xenserver_guest.py's own get_object_ref(fail=False) covers.
func xeGuestResolve(ctx context.Context, conn remoteexec.Connection, args map[string]any, name, uuidArg string) (uuid string, existed bool, failMsg string, err error) {
	var filter string
	if uuidArg != "" {
		filter = "uuid=" + uuidArg
	} else {
		filter = "name-label=" + name
	}
	res, err := xeRun(ctx, conn, args, []string{"vm-list", filter, "params=uuid", "--minimal"})
	if err != nil {
		return "", false, "", fmt.Errorf("xenserver: running vm-list: %w", err)
	}
	if res.RC != 0 {
		return "", false, "", fmt.Errorf("xenserver: vm-list %s: %s", filter, strings.TrimSpace(res.Stderr))
	}
	ids := xeParseList(res.Stdout)
	if len(ids) == 0 {
		if uuidArg != "" {
			return "", false, "VM search: No VM found matching uuid!", nil
		}
		return "", false, "", nil
	}
	if len(ids) > 1 {
		return "", false, fmt.Sprintf("VM search: multiple VMs found matching name %q — use uuid to uniquely specify the VM to manage!", name), nil
	}
	return ids[0], true, "", nil
}

// xeGuestCreate clones or copies the module's own template/template_uuid
// argument into a new VM named name, and clears is-a-template on the
// result (a clone/copy of a template is itself another template until
// explicitly cleared) — see moduleXenserverGuest's own doc comment.
func xeGuestCreate(ctx context.Context, conn remoteexec.Connection, args map[string]any, name string) (uuid, failMsg string, err error) {
	templateUUID := argString(args, "template_uuid", "")
	if templateUUID == "" {
		templateName := argString(args, "template", argString(args, "template_src", ""))
		if templateName == "" {
			return "", "xenserver_guest: template or template_uuid is required to create a new VM", nil
		}
		res, err := xeRun(ctx, conn, args, []string{"vm-list", "name-label=" + templateName, "params=uuid", "--minimal"})
		if err != nil {
			return "", "", err
		}
		if res.RC != 0 {
			return "", "", fmt.Errorf("xenserver: resolving template %s: %s", templateName, strings.TrimSpace(res.Stderr))
		}
		ids := xeParseList(res.Stdout)
		if len(ids) == 0 {
			return "", fmt.Sprintf("xenserver_guest: template %q not found", templateName), nil
		}
		if len(ids) > 1 {
			return "", fmt.Sprintf("xenserver_guest: multiple templates found matching name %q — use template_uuid instead", templateName), nil
		}
		templateUUID = ids[0]
	}

	verb := "vm-copy"
	if argBool(args, "linked_clone", false) {
		verb = "vm-clone"
	}
	res, err := xeRun(ctx, conn, args, []string{verb, "uuid=" + templateUUID, "new-name-label=" + name})
	if err != nil {
		return "", "", err
	}
	if res.RC != 0 {
		return "", fmt.Sprintf("xenserver_guest: creating %s from template: %s", name, strings.TrimSpace(res.Stderr)), nil
	}
	newUUID := strings.TrimSpace(res.Stdout)
	if newUUID == "" {
		return "", "", fmt.Errorf("xenserver: %s did not return a new VM uuid", verb)
	}
	if err := xeParamSet(ctx, conn, args, newUUID, map[string]string{"is-a-template": "false"}); err != nil {
		return "", "", err
	}
	return newUUID, "", nil
}

// xeParamSet runs `xe vm-param-set uuid=<uuid> k=v ...` for every entry
// in sets, in one invocation.
func xeParamSet(ctx context.Context, conn remoteexec.Connection, args map[string]any, uuid string, sets map[string]string) error {
	argv := []string{"vm-param-set", "uuid=" + uuid}
	keys := make([]string, 0, len(sets))
	for k := range sets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		argv = append(argv, k+"="+sets[k])
	}
	res, err := xeRun(ctx, conn, args, argv)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("xenserver: vm-param-set uuid=%s: %s", uuid, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func xeGuestApplyHardware(ctx context.Context, conn remoteexec.Connection, args map[string]any, uuid string, hw map[string]any) error {
	sets := map[string]string{}
	if v, ok := hw["memory_mb"]; ok {
		mb := toInt(v)
		bytes := strconv.FormatInt(int64(mb)*1048576, 10)
		sets["memory-static-max"] = bytes
		sets["memory-dynamic-max"] = bytes
		sets["memory-static-min"] = bytes
		sets["memory-dynamic-min"] = bytes
	}
	if v, ok := hw["num_cpus"]; ok {
		n := strconv.Itoa(toInt(v))
		sets["VCPUs-max"] = n
		sets["VCPUs-at-startup"] = n
	}
	if v, ok := hw["num_cpu_cores_per_socket"]; ok {
		sets["platform:cores-per-socket"] = strconv.Itoa(toInt(v))
	}
	if len(sets) == 0 {
		return nil
	}
	return xeParamSet(ctx, conn, args, uuid, sets)
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

// xeGuestAddDisks creates every disk in the module's own `disks`
// argument as a brand-new VDI+VBD — see moduleXenserverGuest's own doc
// comment for why this port only ever adds disks, never resizes or
// removes an already-existing one.
func xeGuestAddDisks(ctx context.Context, conn remoteexec.Connection, args map[string]any, uuid string, shutOff, force bool) (bool, error) {
	disks := argAnyList(args, "disks")
	if disks == nil {
		disks = argAnyList(args, "disk")
	}
	if len(disks) == 0 {
		return false, nil
	}
	if !shutOff && !force {
		return false, fmt.Errorf("xenserver_guest: VM must be shut down to add disks (or set force=true)")
	}
	added := false
	for i, d := range disks {
		m, ok := d.(map[string]any)
		if !ok {
			continue
		}
		size := xeDiskSizeBytes(m)
		if size == 0 {
			continue
		}
		srUUID := fmt.Sprint(m["sr_uuid"])
		if srUUID == "" || srUUID == "<nil>" {
			srName := fmt.Sprint(m["sr"])
			if srName != "" && srName != "<nil>" {
				res, err := xeRun(ctx, conn, args, []string{"sr-list", "name-label=" + srName, "params=uuid", "--minimal"})
				if err == nil && res.RC == 0 {
					if ids := xeParseList(res.Stdout); len(ids) > 0 {
						srUUID = ids[0]
					}
				}
			}
		}
		if srUUID == "" || srUUID == "<nil>" {
			continue
		}
		nameLabel := fmt.Sprint(m["name"])
		if nameLabel == "<nil>" || nameLabel == "" {
			nameLabel = fmt.Sprint(m["name_label"])
		}
		res, err := xeRun(ctx, conn, args, []string{
			"vdi-create", "sr-uuid=" + srUUID, "name-label=" + nameLabel,
			"virtual-size=" + strconv.FormatInt(size, 10), "type=user",
		})
		if err != nil {
			return added, err
		}
		if res.RC != 0 {
			return added, fmt.Errorf("xenserver: creating disk %d: %s", i, strings.TrimSpace(res.Stderr))
		}
		vdiUUID := strings.TrimSpace(res.Stdout)
		res, err = xeRun(ctx, conn, args, []string{
			"vbd-create", "vm-uuid=" + uuid, "vdi-uuid=" + vdiUUID,
			"device=" + strconv.Itoa(i), "type=Disk", "mode=RW",
		})
		if err != nil {
			return added, err
		}
		if res.RC != 0 {
			return added, fmt.Errorf("xenserver: attaching disk %d: %s", i, strings.TrimSpace(res.Stderr))
		}
		added = true
	}
	return added, nil
}

func xeDiskSizeBytes(m map[string]any) int64 {
	units := []struct {
		key  string
		mult int64
	}{
		{"size_b", 1}, {"size_kb", 1024}, {"size_mb", 1024 * 1024},
		{"size_gb", 1024 * 1024 * 1024}, {"size_tb", 1024 * 1024 * 1024 * 1024},
	}
	for _, u := range units {
		if v, ok := m[u.key]; ok {
			n, _ := strconv.ParseFloat(fmt.Sprint(v), 64)
			if n > 0 {
				return int64(n) * u.mult
			}
		}
	}
	if v, ok := m["size"]; ok {
		s := fmt.Sprint(v)
		for _, u := range []struct {
			suffix string
			mult   int64
		}{{"tb", 1024 * 1024 * 1024 * 1024}, {"gb", 1024 * 1024 * 1024}, {"mb", 1024 * 1024}, {"kb", 1024}, {"b", 1}} {
			if strings.HasSuffix(strings.ToLower(s), u.suffix) {
				n, _ := strconv.ParseFloat(strings.TrimSuffix(strings.ToLower(s), u.suffix), 64)
				return int64(n) * u.mult
			}
		}
		n, _ := strconv.ParseFloat(s, 64)
		return int64(n)
	}
	return 0
}

// xeGuestApplyCDROM reconfigures the VM's own FIRST already-existing
// CD-ROM VBD — see moduleXenserverGuest's own doc comment for why a
// template with no CD-ROM device at all is left without one.
func xeGuestApplyCDROM(ctx context.Context, conn remoteexec.Connection, args map[string]any, uuid string) (bool, error) {
	cdrom := argMapAny(args, "cdrom")
	if len(cdrom) == 0 {
		return false, nil
	}
	vbdsOut, err := run(ctx, conn, xeCmdLine(args, []string{"vbd-list", "vm-uuid=" + uuid, "type=CD", "params=uuid", "--minimal"}))
	if err != nil || strings.TrimSpace(vbdsOut) == "" {
		return false, nil
	}
	vbdUUID := xeParseList(vbdsOut)[0]

	cdType := fmt.Sprint(cdrom["type"])
	if cdType == "none" {
		res, err := xeRun(ctx, conn, args, []string{"vbd-eject", "uuid=" + vbdUUID})
		if err != nil {
			return false, err
		}
		return res.RC == 0, nil
	}
	if cdType == "iso" {
		isoName := fmt.Sprint(cdrom["iso_name"])
		res, err := xeRun(ctx, conn, args, []string{"vdi-list", "name-label=" + isoName, "params=uuid", "--minimal"})
		if err != nil || res.RC != 0 {
			return false, nil
		}
		ids := xeParseList(res.Stdout)
		if len(ids) == 0 {
			return false, nil
		}
		res, err = xeRun(ctx, conn, args, []string{"vbd-insert", "uuid=" + vbdUUID, "vdi-uuid=" + ids[0]})
		if err != nil {
			return false, err
		}
		return res.RC == 0, nil
	}
	return false, nil
}

// xeGuestAddNetworks creates every NIC in the module's own `networks`
// argument as a brand-new VIF, writing any static IPv4/IPv6
// configuration to the VM's own xenstore-data — see
// moduleXenserverGuest's own doc comment for both narrowings (new VIFs
// only; xenstore-data always, never the native guest-agent path).
func xeGuestAddNetworks(ctx context.Context, conn remoteexec.Connection, args map[string]any, uuid string) (bool, error) {
	nets := argAnyList(args, "networks")
	if nets == nil {
		nets = argAnyList(args, "network")
	}
	if len(nets) == 0 {
		return false, nil
	}
	added := false
	for i, n := range nets {
		m, ok := n.(map[string]any)
		if !ok {
			continue
		}
		netName := fmt.Sprint(m["name"])
		if netName == "<nil>" || netName == "" {
			netName = fmt.Sprint(m["name_label"])
		}
		if netName == "<nil>" || netName == "" {
			continue
		}
		res, err := xeRun(ctx, conn, args, []string{"network-list", "name-label=" + netName, "params=uuid", "--minimal"})
		if err != nil {
			return added, err
		}
		ids := xeParseList(res.Stdout)
		if len(ids) == 0 {
			continue
		}
		mac := fmt.Sprint(m["mac"])
		if mac == "<nil>" {
			mac = ""
		}
		vifArgv := []string{"vif-create", "vm-uuid=" + uuid, "network-uuid=" + ids[0], "device=" + strconv.Itoa(i)}
		if mac != "" {
			vifArgv = append(vifArgv, "mac="+mac)
		}
		res, err = xeRun(ctx, conn, args, vifArgv)
		if err != nil {
			return added, err
		}
		if res.RC != 0 {
			return added, fmt.Errorf("xenserver: creating VIF %d: %s", i, strings.TrimSpace(res.Stderr))
		}
		added = true

		xsSets := map[string]string{}
		for _, f := range []string{"ip", "netmask", "gateway", "prefix6", "gateway6"} {
			if v, ok := m[f]; ok && fmt.Sprint(v) != "" {
				xsSets["xenstore-data:vm-data/networks/"+strconv.Itoa(i)+"/"+f] = fmt.Sprint(v)
			}
		}
		if ip6 := m["ip6"]; ip6 != nil && fmt.Sprint(ip6) != "" {
			xsSets["xenstore-data:vm-data/networks/"+strconv.Itoa(i)+"/prefix6"] = fmt.Sprint(ip6)
		}
		if len(xsSets) > 0 {
			if err := xeParamSet(ctx, conn, args, uuid, xsSets); err != nil {
				return added, err
			}
		}
	}
	return added, nil
}

// xeResolveHostUUID resolves a host name to its UUID via `xe host-list
// name-label=<name> params=uuid --minimal`.
func xeResolveHostUUID(ctx context.Context, conn remoteexec.Connection, args map[string]any, name string) (string, error) {
	res, err := xeRun(ctx, conn, args, []string{"host-list", "name-label=" + name, "params=uuid", "--minimal"})
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", nil
	}
	ids := xeParseList(res.Stdout)
	if len(ids) == 0 {
		return "", nil
	}
	return ids[0], nil
}

// argAnyList reads a module argument as a []any (a list of dicts, e.g.
// disks/networks/custom_params), without stringifying its elements the
// way argStringList does.
func argAnyList(args map[string]any, key string) []any {
	v, ok := args[key]
	if !ok {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	return list
}
