package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleVmadm implements Ansible's `vmadm` (community.general) module:
// manages a SmartOS zone/VM's lifecycle via the `vmadm`(1M) CLI —
// `vmadm create -f <payload.json>`, `vmadm start|stop|reboot|delete
// [-F] <uuid>`, and `vmadm lookup -j -o <prop> [uuid=<uuid>|alias=<name>]`
// for reads. There is no library substitution question here at all:
// real vmadm.py already shells out to the `vmadm` binary itself (via
// module.run_command), the same as this port does.
//
// Args: name (alias alias) or uuid — at least one required, matching
// real vmadm's own `required_one_of=[["name", "uuid"]]`; uuid="*"
// operates on every VM (state transitions only — state=created is
// rejected in that case, matching real manage_all_vms's own check);
// state (present|running|stopped|created|absent|deleted|restarted|
// rebooted, default running) — mapped to one of four underlying
// vm_states (running/stopped/deleted/rebooted) exactly as real
// vmadm.py's own main() does: present/running->running,
// stopped/created->stopped, absent/deleted->deleted,
// restarted/rebooted->rebooted; force (bool) — `-F` on stop/delete
// only (not start/reboot — matching real vmadm.py's own
// `cmds = {"stopped": [..., True], "running": [..., False], "deleted":
// [..., True], "rebooted": [..., False]}` forceable flags exactly);
// plus every other vmadm(1M) VM property real vmadm.py documents
// (brand, image_uuid, ram, quota, nics, disks, customer_metadata,
// ... — see ansible-doc's own OPTIONS list; not individually
// enumerated in this Go doc comment).
//
// Deviation — property pass-through is GENERIC, not a fixed field
// list: real vmadm.py's own create_payload() builds its JSON payload
// as `{k: v for k, v in module.params.items() if k not in
// ("force", "state") and v}` — i.e. EVERY declared module argument
// (Ansible's own argument_spec always populates module.params with
// every declared option, None-valued when the caller didn't set it)
// whose value is Python-truthy, keyed by its OWN argument name. This
// port's args map, by this whole package's own convention (see
// module.go's own doc comment and ipa_common.go's "already resolved
// by the caller" note elsewhere), contains ONLY what the caller
// actually passed — so replicating real vmadm.py's dict comprehension
// over `module.params.items()` reduces here to: every key in args
// except "force"/"state", filtered to non-empty/non-zero/non-false
// (vmadmTruthy), passed straight through as a JSON payload field under
// its own literal Go args-map key. This is not a fixed enumeration of
// vmadm(1M)'s own ~70 documented properties (unlike real vmadm.py's
// own explicit `properties = {"str": [...], "bool": [...], ...}`
// tables) — it is a faithful reproduction of what that dict
// comprehension actually DOES given this port's own args-map
// convention, without needing to hardcode every property name.
//
// Deviation — "name" is passed to vmadm's own JSON payload as the
// LITERAL KEY "name", not "alias": verified directly against real
// vmadm.py's own create_payload() source, which passes
// module.params.items() through UNTRANSLATED — module.params' own key
// for this option is "name" (its `aliases: ["alias"]` only lets a
// playbook SPELL it "alias:", it does not rename the key vmadm.py
// itself later uses). Whether real `vmadm`(1M) actually recognizes a
// "name" property the same way it recognizes "alias" could not be
// verified against a live SmartOS host from this environment; this
// port reproduces real vmadm.py's own literal payload key exactly
// rather than silently "fixing" it to "alias", per this project's own
// hard rule to replicate verified source behavior rather than what
// seems more sensible.
//
// A new VM is created by writing the JSON payload to a temp file ON
// THE TARGET (via `cat > <tmp>` fed the payload over Exec's own stdin,
// then `chmod 400`, matching real vmadm.py's own `os.chmod(fname,
// 0o400)` — the payload may contain sensitive fields like
// spice_password/vnc_password) and running `vmadm create -f <tmp>`;
// the temp file is removed afterward (best-effort, via
// Connection.Remove) whether or not creation succeeded, matching real
// vmadm.py's own `finally`-adjacent `os.unlink(payload_file)` (real
// vmadm.py actually fails hard if unlink fails, since the payload may
// hold secrets — this port's own Remove is best-effort only, since
// Connection.Remove has no signal this port trusts enough to fail an
// otherwise-successful VM creation over; a narrow, documented gap).
// `vmadm create`'s own success message is printed to STDERR as
// "Successfully created VM <uuid>" (matching real vmadm.py's own
// regex against `stderr`, not `stdout`) — parsed here the same way.
//
// If vm_state isn't "running" right after a fresh create, this port
// transitions the newly created VM to it immediately afterward (stop/
// delete/reboot), matching real new_vm()'s own follow-up
// set_vm_state() call.
//
// uuid="*" (manage_all_vms): fails (state=created only valid for a
// single VM) if state maps to... no, only when the ORIGINAL state
// argument is literally "created" (checked before the vm_state
// mapping, matching real vmadm.py's own `if state == "created":
// module.fail_json(...)` against module.params["state"] itself, not
// vm_state). Otherwise every VM's own current state (from `vmadm
// lookup -j -o uuid` then per-uuid `-o state`) is transitioned toward
// vm_state; Changed is true if ANY VM actually transitioned.
func moduleVmadm(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	uuid := argString(args, "uuid", "")
	name := argString(args, "name", argString(args, "alias", ""))
	if uuid == "" && name == "" {
		return Result{}, errArg("vmadm: one of name, uuid is required")
	}
	state := argString(args, "state", "running")
	vmState, err := vmadmVMState(state)
	if err != nil {
		return Result{}, err
	}
	force := argBool(args, "force", false)

	if uuid != "" && uuid != "*" && !vmadmValidUUID(uuid) {
		return Fail("vmadm: no valid UUID(s) found for: uuid"), nil
	}
	if iu := argString(args, "image_uuid", ""); iu != "" && iu != "*" && !vmadmValidUUID(iu) {
		return Fail("vmadm: no valid UUID(s) found for: image_uuid"), nil
	}

	if uuid == "" {
		found, err := vmadmLookupUUIDByAlias(ctx, conn, name)
		if err != nil {
			return Result{}, err
		}
		if found == "" && vmState == "deleted" {
			return Ok("").WithExtra("name", name), nil
		}
		uuid = found
	}

	if uuid == "*" {
		if state == "created" {
			return Fail(`vmadm: State "created" is only valid for tasks with a single VM`), nil
		}
		changed, err := vmadmManageAll(ctx, conn, vmState, force)
		if err != nil {
			return Result{}, err
		}
		res := Result{Changed: changed}
		return res.WithExtra("state", state).WithExtra("uuid", uuid), nil
	}

	extra := map[string]any{"state": state}
	if name != "" {
		extra["name"] = name
	}

	currentState, err := vmadmGetProp(ctx, conn, uuid, "state")
	if err != nil {
		return Result{}, err
	}

	var changed bool
	if currentState == "" && vmState == "deleted" {
		changed = false
	} else if currentState == "" {
		newUUID, cerr := vmadmCreate(ctx, conn, args, vmState)
		if cerr != nil {
			return Result{}, cerr
		}
		changed = true
		uuid = newUUID
	} else {
		changed, err = vmadmTransition(ctx, conn, uuid, vmState, force)
		if err != nil {
			return Result{}, err
		}
	}

	extra["uuid"] = uuid
	res := Result{Changed: changed}
	for k, v := range extra {
		res = res.WithExtra(k, v)
	}
	return res, nil
}

func vmadmVMState(state string) (string, error) {
	switch state {
	case "present", "running":
		return "running", nil
	case "stopped", "created":
		return "stopped", nil
	case "absent", "deleted":
		return "deleted", nil
	case "restarted", "rebooted":
		return "rebooted", nil
	default:
		return "", errArg("vmadm: state must be one of present, running, stopped, created, absent, deleted, restarted, rebooted, got %q", state)
	}
}

var vmadmUUIDRe = regexp.MustCompile(`(?i)^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)

func vmadmValidUUID(s string) bool {
	return vmadmUUIDRe.MatchString(s)
}

// vmadmLookupUUIDByAlias runs `vmadm lookup -j -o uuid alias=<name>`
// and returns the first match's uuid, or "" if none, matching real
// vmadm.py's own get_vm_uuid().
func vmadmLookupUUIDByAlias(ctx context.Context, conn remoteexec.Connection, name string) (string, error) {
	out, err := run(ctx, conn, "vmadm lookup -j -o uuid "+shellQuote("alias="+name))
	if err != nil {
		return "", fmt.Errorf("vmadm: could not retrieve UUID of %s: %w", name, err)
	}
	var rows []map[string]any
	if jerr := json.Unmarshal([]byte(out), &rows); jerr != nil {
		return "", fmt.Errorf("vmadm: invalid JSON returned by vmadm for uuid lookup of %s: %w", name, jerr)
	}
	if len(rows) == 0 {
		return "", nil
	}
	if u, ok := rows[0]["uuid"].(string); ok {
		return u, nil
	}
	return "", nil
}

// vmadmGetProp runs `vmadm lookup -j -o <prop> uuid=<uuid>` and
// returns that VM's own value for prop, or "" if the VM doesn't exist,
// matching real vmadm.py's own get_vm_prop().
func vmadmGetProp(ctx context.Context, conn remoteexec.Connection, uuid, prop string) (string, error) {
	out, err := run(ctx, conn, "vmadm lookup -j -o "+shellQuote(prop)+" "+shellQuote("uuid="+uuid))
	if err != nil {
		return "", fmt.Errorf("vmadm: could not perform lookup of %s on %s: %w", prop, uuid, err)
	}
	var rows []map[string]any
	if jerr := json.Unmarshal([]byte(out), &rows); jerr != nil {
		return "", fmt.Errorf("vmadm: invalid JSON returned by vmadm for uuid lookup of %s: %w", prop, jerr)
	}
	if len(rows) == 0 {
		return "", nil
	}
	if v, ok := rows[0][prop]; ok && v != nil {
		return fmt.Sprint(v), nil
	}
	return "", nil
}

// vmadmAllUUIDs runs `vmadm lookup -j -o uuid` (no filter) and returns
// every VM's uuid, matching real vmadm.py's own get_all_vm_uuids().
func vmadmAllUUIDs(ctx context.Context, conn remoteexec.Connection) ([]string, error) {
	out, err := run(ctx, conn, "vmadm lookup -j -o uuid")
	if err != nil {
		return nil, fmt.Errorf("vmadm: failed to get VMs list: %w", err)
	}
	var rows []map[string]any
	if jerr := json.Unmarshal([]byte(out), &rows); jerr != nil {
		return nil, fmt.Errorf("vmadm: could not retrieve VM UUIDs: %w", jerr)
	}
	out2 := make([]string, 0, len(rows))
	for _, r := range rows {
		if u, ok := r["uuid"].(string); ok {
			out2 = append(out2, u)
		}
	}
	return out2, nil
}

// vmadmTruthy mirrors Python truthiness for the value types that can
// appear in this port's args map — see moduleVmadm's own doc comment
// on why this replaces real vmadm.py's own fixed property-name tables.
func vmadmTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case []any:
		return len(x) != 0
	case map[string]any:
		return len(x) != 0
	default:
		return true
	}
}

// vmadmBuildPayload builds the JSON payload for `vmadm create`, from
// every args key except force/state whose value is vmadmTruthy — see
// moduleVmadm's own doc comment.
func vmadmBuildPayload(args map[string]any) map[string]any {
	vmdef := map[string]any{}
	for k, v := range args {
		if k == "force" || k == "state" {
			continue
		}
		if vmadmTruthy(v) {
			vmdef[k] = v
		}
	}
	return vmdef
}

var vmadmCreatedRe = regexp.MustCompile(`^Successfully created VM (\S+)`)

// vmadmCreate writes args' own vmdef payload to a temp file on the
// target, runs `vmadm create -f <tmp>`, and — unless vmState is
// already "running" — immediately transitions the freshly created VM
// to vmState, matching real vmadm.py's own new_vm().
func vmadmCreate(ctx context.Context, conn remoteexec.Connection, args map[string]any, vmState string) (string, error) {
	vmdef := vmadmBuildPayload(args)
	payload, err := json.Marshal(vmdef)
	if err != nil {
		return "", fmt.Errorf("vmadm: could not create valid JSON payload: %w", err)
	}

	tmp := conn.TempPath("vmadm-payload.json")
	if tmp == "" {
		tmp = "/tmp/vmadm-payload.json"
	}
	writeRes, err := conn.Exec(ctx, "umask 077 && cat > "+shellQuote(tmp)+" && chmod 400 "+shellQuote(tmp), strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	if writeRes.RC != 0 {
		return "", fmt.Errorf("vmadm: could not save JSON payload: %s", strings.TrimSpace(writeRes.Stderr))
	}
	defer func() { _ = conn.Remove(ctx, tmp) }()

	createRes, err := conn.Exec(ctx, "vmadm create -f "+shellQuote(tmp), nil)
	if err != nil {
		return "", err
	}
	if createRes.RC != 0 {
		return "", fmt.Errorf("vmadm: could not create VM: %s", strings.TrimSpace(createRes.Stderr))
	}
	m := vmadmCreatedRe.FindStringSubmatch(strings.TrimSpace(createRes.Stderr))
	if m == nil {
		return "", fmt.Errorf("vmadm: could not retrieve UUID of newly created(?) VM")
	}
	newUUID := m[1]
	if !vmadmValidUUID(newUUID) {
		return "", fmt.Errorf("vmadm: invalid UUID for VM %s?", newUUID)
	}

	if vmState != "running" {
		if _, _, err := vmadmSetState(ctx, conn, newUUID, vmState, false); err != nil {
			return "", err
		}
	}
	return newUUID, nil
}

// vmadmSetState transitions uuid to vmState (start/stop/delete/reboot),
// matching real vmadm.py's own set_vm_state(): already=true means the
// VM was already in vmState (no command run at all); ran=false with a
// nil error but already=false means the command ran but did not print
// its own "Successfully..." confirmation (treated as a Fail by the
// caller, matching real vmadm.py's own vm_state_transition()).
func vmadmSetState(ctx context.Context, conn remoteexec.Connection, uuid, vmState string, force bool) (already, ran bool, err error) {
	cur, err := vmadmGetProp(ctx, conn, uuid, "state")
	if err != nil {
		return false, false, err
	}
	if cur != "" && cur == vmState {
		return true, false, nil
	}

	var command string
	var forceable bool
	switch vmState {
	case "stopped":
		command, forceable = "stop", true
	case "running":
		command, forceable = "start", false
	case "deleted":
		command, forceable = "delete", true
	case "rebooted":
		command, forceable = "reboot", false
	default:
		return false, false, fmt.Errorf("vmadm: unknown vm_state %q", vmState)
	}

	argv := []string{"vmadm", command}
	if force && forceable {
		argv = append(argv, "-F")
	}
	argv = append(argv, uuid)
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	res, err := conn.Exec(ctx, strings.Join(quoted, " "), nil)
	if err != nil {
		return false, false, err
	}
	ok := strings.HasPrefix(strings.TrimSpace(res.Stderr), "Successfully")
	return false, ok, nil
}

// vmadmTransition wraps vmadmSetState the way real vmadm.py's own
// vm_state_transition() does: already-there is Changed=false with no
// error; a ran-but-not-confirmed transition is a Fail.
func vmadmTransition(ctx context.Context, conn remoteexec.Connection, uuid, vmState string, force bool) (bool, error) {
	already, ran, err := vmadmSetState(ctx, conn, uuid, vmState, force)
	if err != nil {
		return false, err
	}
	if already {
		return false, nil
	}
	if !ran {
		return false, fmt.Errorf("vmadm: failed to set VM %s to state %s", uuid, vmState)
	}
	return true, nil
}

// vmadmManageAll transitions every existing VM toward vmState,
// matching real vmadm.py's own manage_all_vms(): a VM already deleted
// (no current state, requesting vmState=deleted) contributes no
// change; every other VM is transitioned and anyChanged is the OR of
// every individual transition.
func vmadmManageAll(ctx context.Context, conn remoteexec.Connection, vmState string, force bool) (bool, error) {
	uuids, err := vmadmAllUUIDs(ctx, conn)
	if err != nil {
		return false, err
	}
	anyChanged := false
	for _, u := range uuids {
		cur, err := vmadmGetProp(ctx, conn, u, "state")
		if err != nil {
			return false, err
		}
		if cur == "" && vmState == "deleted" {
			continue
		}
		changed, err := vmadmTransition(ctx, conn, u, vmState, force)
		if err != nil {
			return false, err
		}
		if changed {
			anyChanged = true
		}
	}
	return anyChanged, nil
}
