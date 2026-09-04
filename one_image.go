package modules

import (
	"context"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

// Real OpenNebula image STATE numeric codes, verified against real
// one_image.py's own IMAGE_STATES list (its own index() calls are what
// this port mirrors, not a separate guess): INIT=0, READY=1, USED=2,
// DISABLED=3, LOCKED=4, ERROR=5, CLONE=6, DELETE=7, USED_PERS=8,
// LOCKED_USED=9, LOCKED_USED_PERS=10.
var oneImageStates = []string{
	"INIT", "READY", "USED", "DISABLED", "LOCKED", "ERROR", "CLONE",
	"DELETE", "USED_PERS", "LOCKED_USED", "LOCKED_USED_PERS",
}

const (
	oneImageStateReady    = 1
	oneImageStateDisabled = 3
	oneImageStateError    = 5
)

// moduleOneImage implements Ansible's `one_image` module: manages an
// OpenNebula image's lifecycle (present/absent/cloned/renamed) plus its
// enabled/persistent flags, via the `oneimage` CLI (see one_common.go's
// own doc comment).
//
// Args: id (int) / name (string) — mutually exclusive, matching real
// one_image's own mutually_exclusive; state (present|absent|cloned|
// renamed, default "present"); enabled (bool, tri-state: absent means
// "leave alone", matching real one_image's own `enabled=None` default);
// new_name; persistent (bool, tri-state); create (bool) — required_if
// with template+datastore_id+name when true, matching real one_image
// exactly; template (string, raw OpenNebula template text);
// datastore_id (int); wait_timeout (int, default 60) — accepted, but
// this port does NOT poll for READY/DELETE convergence after create/
// clone/delete the way real one_image's own wait_for_ready/
// wait_for_delete do (same documented-gap stance as one_host.go's own
// wait note): facts returned right after a create/clone/delete may
// reflect a transitional state (LOCKED/CLONE/DELETE) rather than the
// converged one.
//
// Identifying an existing image after create/clone (this port has no
// stdout convention for `oneimage create`/`oneimage clone` to trust —
// unlike onetemplate's own verified "VM ID: N" convention, oneimage's
// own create/clone stdout shape could not be confirmed against a live
// binary): this port always re-lists and resolves the resulting image
// BY NAME afterwards (the name it just created/cloned to is always
// known — required_if guarantees `name` for create; new_name defaults
// to "Copy of <name>" for clone, matching real one_image exactly) —
// deliberately avoiding any assumption about create/clone's own stdout
// format.
//
// Facts (Extra fields) are populated only when state is present/cloned/
// renamed, matching real one_image's own RETURN block. Real
// one_image's own module_utils get_image_info() actually assigns
// "user_id"/"user_name" keys (from image.UID/image.UNAME) — NOT
// "owner_id"/"owner_name" as its own RETURN documentation block
// promises; this port matches what the CODE actually produces
// (user_id/user_name), a genuine real upstream doc/code mismatch this
// project's own "bibliographie avant" rule caught by reading the
// source, not the misleading RETURN docs (see rundeck_project.go's own
// analogous label/description doc-vs-code note).
func moduleOneImage(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	_, hasID := args["id"]
	_, hasName := args["name"]
	if hasID && hasName {
		return Result{}, errArg("one_image: id and name are mutually exclusive")
	}
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent", "cloned", "renamed":
	default:
		return Result{}, errArg("one_image: state must be one of present, absent, cloned, renamed, got %q", state)
	}
	if state == "renamed" && !hasID {
		return Result{}, errArg("one_image: id is required when state is renamed")
	}
	create := argBool(args, "create", false)
	if create {
		_, hasDS := args["datastore_id"]
		if argString(args, "template", "") == "" || argString(args, "name", "") == "" || !hasDS {
			return Result{}, errArg("one_image: template, datastore_id and name are required when create is true")
		}
	}

	url := oneAuth(args)
	if res, ok := oneRequireBinary(ctx, conn, "oneimage", "one_image"); !ok {
		return res, nil
	}

	image, found, err := oneImageResolve(ctx, conn, url, args)
	if err != nil {
		return Result{}, err
	}

	if !found && state != "absent" {
		switch {
		case create:
			name := argString(args, "name", "")
			template := argString(args, "template", "")
			datastoreID := argInt(args, "datastore_id", 0)
			body := "NAME = \"" + name + "\"\n" + template
			res, err := oneRunStdin(ctx, conn, url, "oneimage", body, "create", "-d", strconv.Itoa(datastoreID), "-")
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("one_image: creating image: " + oneErrMsg(res)), nil
			}
			pool, err := oneListXML(ctx, conn, url, "oneimage")
			if err != nil {
				return Result{}, err
			}
			created, ok := oneResolveByName(pool, "IMAGE", name)
			if !ok {
				return Fail("one_image: image was created but could not be found afterwards"), nil
			}
			out := Changed("")
			return oneImageResultWithFacts(out, created), nil

		case hasID:
			return Fail("one_image: There is no image with id=" + argString(args, "id", "")), nil

		case hasName:
			return Fail("one_image: There is no image with name=" + argString(args, "name", "")), nil

		default:
			return Ok(""), nil
		}
	}

	if state == "absent" {
		if !found {
			return Ok(""), nil
		}
		if image.childInt("RUNNING_VMS") > 0 {
			return Fail("one_image: Cannot delete image. There are " + image.childText("RUNNING_VMS") + " VMs using it."), nil
		}
		res, err := oneRun(ctx, conn, url, "oneimage", "delete", image.childText("ID"))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("one_image: deleting image: " + oneErrMsg(res)), nil
		}
		return Changed(""), nil
	}

	changed := false

	if _, ok := args["persistent"]; ok {
		persistent := argBool(args, "persistent", false)
		st := image.childInt("STATE")
		if st != oneImageStateReady && st != oneImageStateDisabled && st != oneImageStateError {
			return Fail("one_image: Cannot change persistence for " + oneImageStateName(st) + " image!"), nil
		}
		wantChange := (persistent && st != oneImageStateReady) || (!persistent && st != oneImageStateDisabled)
		if wantChange {
			verb := "nonpersistent"
			if persistent {
				verb = "persistent"
			}
			res, err := oneRun(ctx, conn, url, "oneimage", verb, image.childText("ID"))
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("one_image: " + oneErrMsg(res)), nil
			}
			changed = true
			image, _, err = oneShowXML(ctx, conn, url, "oneimage", image.childText("ID"))
			if err != nil {
				return Result{}, err
			}
		}
	}

	if _, ok := args["enabled"]; ok {
		enabled := argBool(args, "enabled", false)
		st := image.childInt("STATE")
		if st != oneImageStateReady && st != oneImageStateDisabled && st != oneImageStateError {
			return Fail("one_image: Cannot change enabled state for " + oneImageStateName(st) + " image!"), nil
		}
		wantChange := (enabled && st != oneImageStateReady) || (!enabled && st != oneImageStateDisabled)
		if wantChange {
			verb := "disable"
			if enabled {
				verb = "enable"
			}
			res, err := oneRun(ctx, conn, url, "oneimage", verb, image.childText("ID"))
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("one_image: " + oneErrMsg(res)), nil
			}
			changed = true
			image, _, err = oneShowXML(ctx, conn, url, "oneimage", image.childText("ID"))
			if err != nil {
				return Result{}, err
			}
		}
	}

	switch state {
	case "cloned":
		newName := argString(args, "new_name", "")
		if newName == "" {
			newName = "Copy of " + image.childText("NAME")
		}
		pool, err := oneListXML(ctx, conn, url, "oneimage")
		if err != nil {
			return Result{}, err
		}
		if existing, ok := oneResolveByName(pool, "IMAGE", newName); ok {
			out := Result{Changed: changed}
			return oneImageResultWithFacts(out, existing), nil
		}
		if image.childInt("STATE") == oneImageStateDisabled {
			return Fail("one_image: Cannot clone DISABLED image"), nil
		}
		res, err := oneRun(ctx, conn, url, "oneimage", "clone", image.childText("ID"), newName)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("one_image: cloning image: " + oneErrMsg(res)), nil
		}
		pool, err = oneListXML(ctx, conn, url, "oneimage")
		if err != nil {
			return Result{}, err
		}
		cloned, ok := oneResolveByName(pool, "IMAGE", newName)
		if !ok {
			return Fail("one_image: image was cloned but could not be found afterwards"), nil
		}
		out := Changed("")
		return oneImageResultWithFacts(out, cloned), nil

	case "renamed":
		newName := argString(args, "new_name", "")
		if newName == "" {
			return Result{}, errArg("one_image: new_name is required when state is renamed")
		}
		if newName == image.childText("NAME") {
			out := Result{Changed: changed}
			return oneImageResultWithFacts(out, image), nil
		}
		pool, err := oneListXML(ctx, conn, url, "oneimage")
		if err != nil {
			return Result{}, err
		}
		if existing, ok := oneResolveByName(pool, "IMAGE", newName); ok {
			return Fail("one_image: Name '" + newName + "' is already taken by IMAGE with id=" + existing.childText("ID")), nil
		}
		res, err := oneRun(ctx, conn, url, "oneimage", "rename", image.childText("ID"), newName)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("one_image: renaming image: " + oneErrMsg(res)), nil
		}
		image, _, err = oneShowXML(ctx, conn, url, "oneimage", image.childText("ID"))
		if err != nil {
			return Result{}, err
		}
		out := Changed("")
		return oneImageResultWithFacts(out, image), nil
	}

	out := Result{Changed: changed}
	return oneImageResultWithFacts(out, image), nil
}

// oneImageResolve resolves the target image the same way real
// one_image's own get_image_instance does: id (even 0) takes
// precedence over name.
func oneImageResolve(ctx context.Context, conn remoteexec.Connection, url string, args map[string]any) (oneXMLNode, bool, error) {
	if v, ok := args["id"]; ok && v != nil {
		return oneShowXML(ctx, conn, url, "oneimage", fmtAny(v))
	}
	name := argString(args, "name", "")
	if name == "" {
		return oneXMLNode{}, false, nil
	}
	pool, err := oneListXML(ctx, conn, url, "oneimage")
	if err != nil {
		return oneXMLNode{}, false, err
	}
	item, ok := oneResolveByName(pool, "IMAGE", name)
	return item, ok, nil
}

func oneImageStateName(st int) string {
	if st >= 0 && st < len(oneImageStates) {
		return oneImageStates[st]
	}
	return "UNKNOWN"
}

// oneImageResultWithFacts attaches image's facts to res, matching real
// one_image's own get_image_info() key names — see this file's own doc
// comment on the user_id/user_name-vs-owner_id/owner_name discrepancy.
func oneImageResultWithFacts(res Result, image oneXMLNode) Result {
	res = res.WithExtra("id", image.childInt("ID"))
	res = res.WithExtra("name", image.childText("NAME"))
	res = res.WithExtra("state", oneImageStateName(image.childInt("STATE")))
	runningVMs := image.childInt("RUNNING_VMS")
	res = res.WithExtra("running_vms", runningVMs)
	res = res.WithExtra("used", runningVMs > 0)
	res = res.WithExtra("user_name", image.childText("UNAME"))
	res = res.WithExtra("user_id", image.childInt("UID"))
	res = res.WithExtra("group_name", image.childText("GNAME"))
	res = res.WithExtra("group_id", image.childInt("GID"))
	if perms, ok := image.child("PERMISSIONS"); ok {
		res = res.WithExtra("permissions", map[string]any{
			"owner_u": perms.childText("OWNER_U"), "owner_m": perms.childText("OWNER_M"), "owner_a": perms.childText("OWNER_A"),
			"group_u": perms.childText("GROUP_U"), "group_m": perms.childText("GROUP_M"), "group_a": perms.childText("GROUP_A"),
			"other_u": perms.childText("OTHER_U"), "other_m": perms.childText("OTHER_M"), "other_a": perms.childText("OTHER_A"),
		})
	}
	res = res.WithExtra("type", image.childText("TYPE"))
	res = res.WithExtra("disk_type", image.childText("DISK_TYPE"))
	res = res.WithExtra("persistent", image.childInt("PERSISTENT"))
	res = res.WithExtra("source", image.childText("SOURCE"))
	res = res.WithExtra("path", image.childText("PATH"))
	fstype := image.childText("FSTYPE")
	if fstype == "" {
		fstype = "Null"
	}
	res = res.WithExtra("fstype", fstype)
	res = res.WithExtra("size", image.childInt("SIZE"))
	res = res.WithExtra("cloning_ops", image.childInt("CLONING_OPS"))
	res = res.WithExtra("cloning_id", image.childInt("CLONING_ID"))
	res = res.WithExtra("target_snapshot", image.childInt("TARGET_SNAPSHOT"))
	res = res.WithExtra("datastore_id", image.childInt("DATASTORE_ID"))
	res = res.WithExtra("datastore", image.childText("DATASTORE"))
	res = res.WithExtra("vms", oneChildIDList(image, "VMS"))
	res = res.WithExtra("clones", oneChildIDList(image, "CLONES"))
	res = res.WithExtra("app_clones", oneChildIDList(image, "APP_CLONES"))
	if snaps, ok := image.child("SNAPSHOTS"); ok {
		res = res.WithExtra("snapshots", oneImageSnapshots(snaps))
	} else {
		res = res.WithExtra("snapshots", []any{})
	}
	if tmpl, ok := image.child("TEMPLATE"); ok {
		res = res.WithExtra("template", tmpl.toMap())
	}
	return res
}

// oneChildIDList reads e.g. <VMS><ID>1</ID><ID>2</ID></VMS> into
// []any{1, 2}, matching real one_image's own get_image_list_id.
func oneChildIDList(n oneXMLNode, tag string) []any {
	c, ok := n.child(tag)
	if !ok {
		return []any{}
	}
	var out []any
	for _, id := range c.children("ID") {
		v, err := strconv.Atoi(id.text())
		if err != nil {
			out = append(out, id.text())
			continue
		}
		out = append(out, v)
	}
	if out == nil {
		out = []any{}
	}
	return out
}

// oneImageSnapshots reads a SNAPSHOTS node into the same shape real
// one_image's own get_image_snapshots_list produces.
func oneImageSnapshots(n oneXMLNode) []any {
	allowOrphans := n.childText("ALLOW_ORPHANS")
	if allowOrphans == "" {
		allowOrphans = "Null"
	}
	var out []any
	for _, s := range n.children("SNAPSHOT") {
		active := s.childText("ACTIVE")
		if active == "" {
			active = "Null"
		}
		children := s.childText("CHILDREN")
		if children == "" {
			children = "Null"
		}
		name := s.childText("NAME")
		if name == "" {
			name = "Null"
		}
		out = append(out, map[string]any{
			"date": s.childText("DATE"), "parent": s.childText("PARENT"), "size": s.childText("SIZE"),
			"allow_orhans": allowOrphans, "children": children, "active": active, "name": name,
		})
	}
	if out == nil {
		out = []any{}
	}
	return out
}
