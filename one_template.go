package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleOneTemplate implements Ansible's `one_template` module:
// creates, updates, or deletes an OpenNebula VM template, via the
// `onetemplate` CLI (see one_common.go's own doc comment).
//
// Args: id (int) / name (string) — mutually exclusive, and exactly one
// is required (matching real one_template's own required_one_of);
// state (present|absent, default "present"); template (string, raw
// OpenNebula template text — required when state=present, matching
// real one_template's own required_if); filter
// (user_primary_group|user|all|user_groups, default "user") — real
// one_template's own filter maps to a pyone-specific pagination "owner
// scope" flag (user_primary_group=-4, user=-3, all=-2,
// user_groups=-1) that has no equivalent `onetemplate list`/`show`
// flag this port could verify; `onetemplate` always lists templates
// the authenticated user can see (matching filter="all"'s own
// broadest scope) — this port accepts `filter` for argument-shape
// compatibility but it has NO EFFECT, a documented, honestly-scoped
// gap rather than a guess at an unverified CLI flag.
//
// state=absent: `onetemplate delete <id>` if found, else no-op.
// state=present, not found (only possible when addressed by name — an
// unresolved id fails outright, matching real one_template exactly):
// `onetemplate create -` (stdin `NAME = "<name>"\n<template>`), then
// re-resolved by name for facts.
// state=present, found: ALWAYS runs `onetemplate update <id> -` (a
// full replace — no `--append`, matching real one_template's own
// `one.template.update(id, data, 0)` REPLACE mode exactly) — even if
// the content would not actually change, faithfully matching real
// one_template's own unconditional-write-then-diff behavior (see this
// file's own doc comment on Changed below); Changed is computed by
// comparing the TEMPLATE block's own structural map (via oneXMLNode's
// toMap) before and after, matching real one_template's own `result["
// template"] != template.TEMPLATE` dict comparison.
//
// Extra fields (only when state=present): id, name, template (the
// TEMPLATE block's own structural map), user_name, user_id,
// group_name, group_id — matching real one_template's own
// get_template_info() key names exactly (note: real one_template's own
// RETURN docs additionally promise owner_id/owner_name, which the code
// never actually sets — the same real doc-vs-code mismatch this port
// already found and documented in one_image.go).
func moduleOneTemplate(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	_, hasID := args["id"]
	_, hasName := args["name"]
	if hasID && hasName {
		return Result{}, errArg("one_template: id and name are mutually exclusive")
	}
	if !hasID && !hasName {
		return Result{}, errArg("one_template: one of id or name is required")
	}
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent":
	default:
		return Result{}, errArg("one_template: state must be one of present, absent, got %q", state)
	}
	templateData := argString(args, "template", "")
	if state == "present" && templateData == "" {
		return Result{}, errArg("one_template: template is required when state is present")
	}

	url := oneAuth(args)
	if res, ok := oneRequireBinary(ctx, conn, "onetemplate", "one_template"); !ok {
		return res, nil
	}

	tmpl, found, err := oneVMTemplateResolve(ctx, conn, url, args)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !found {
			return Ok(""), nil
		}
		res, err := oneRun(ctx, conn, url, "onetemplate", "delete", tmpl.childText("ID"))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("one_template: deleting template: " + oneErrMsg(res)), nil
		}
		return Changed(""), nil
	}

	if !found {
		if hasID {
			return Fail("one_template: There is no template with id=" + argString(args, "id", "")), nil
		}
		name := argString(args, "name", "")
		body := "NAME = \"" + name + "\"\n" + templateData
		res, err := oneRunStdin(ctx, conn, url, "onetemplate", body, "create", "-")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("one_template: creating template: " + oneErrMsg(res)), nil
		}
		pool, err := oneListXML(ctx, conn, url, "onetemplate")
		if err != nil {
			return Result{}, err
		}
		created, ok := oneResolveByName(pool, "VMTEMPLATE", name)
		if !ok {
			return Fail("one_template: template was created but could not be found afterwards"), nil
		}
		out := Changed("")
		return oneTemplateResultWithFacts(out, created), nil
	}

	beforeMap := map[string]any{}
	if before, ok := tmpl.child("TEMPLATE"); ok {
		beforeMap = before.toMap()
	}
	res, err := oneRunStdin(ctx, conn, url, "onetemplate", templateData, "update", tmpl.childText("ID"), "-")
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("one_template: updating template: " + oneErrMsg(res)), nil
	}
	updated, _, err := oneShowXML(ctx, conn, url, "onetemplate", tmpl.childText("ID"))
	if err != nil {
		return Result{}, err
	}
	afterMap := map[string]any{}
	if after, ok := updated.child("TEMPLATE"); ok {
		afterMap = after.toMap()
	}
	out := Result{Changed: !oneMapsEqual(beforeMap, afterMap)}
	return oneTemplateResultWithFacts(out, updated), nil
}

// oneVMTemplateResolve resolves id/name against `onetemplate list -x`'s
// VMTEMPLATE_POOL/VMTEMPLATE, matching real one_template's own
// get_template_instance (id, even falsy 0, is never actually usable
// here since real one_template's own `if requested_id:` is falsy for
// id=0 too — a real narrowing this port matches: id=0 is treated as
// "no id given" here, same as real one_template).
func oneVMTemplateResolve(ctx context.Context, conn remoteexec.Connection, url string, args map[string]any) (oneXMLNode, bool, error) {
	if v, ok := args["id"]; ok && v != nil && fmtAny(v) != "0" {
		return oneShowXML(ctx, conn, url, "onetemplate", fmtAny(v))
	}
	name := argString(args, "name", "")
	if name == "" {
		return oneXMLNode{}, false, nil
	}
	pool, err := oneListXML(ctx, conn, url, "onetemplate")
	if err != nil {
		return oneXMLNode{}, false, err
	}
	item, ok := oneResolveByName(pool, "VMTEMPLATE", name)
	return item, ok, nil
}

func oneTemplateResultWithFacts(res Result, tmpl oneXMLNode) Result {
	res = res.WithExtra("id", tmpl.childInt("ID"))
	res = res.WithExtra("name", tmpl.childText("NAME"))
	if t, ok := tmpl.child("TEMPLATE"); ok {
		res = res.WithExtra("template", t.toMap())
	} else {
		res = res.WithExtra("template", map[string]any{})
	}
	res = res.WithExtra("user_name", tmpl.childText("UNAME"))
	res = res.WithExtra("user_id", tmpl.childInt("UID"))
	res = res.WithExtra("group_name", tmpl.childText("GNAME"))
	res = res.WithExtra("group_id", tmpl.childInt("GID"))
	return res
}

// oneMapsEqual does a plain deep-equal over two decoded oneXMLNode
// toMap() results — used for the before/after Changed comparison.
func oneMapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		if !oneValuesEqual(av, bv) {
			return false
		}
	}
	return true
}

func oneValuesEqual(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		return ok && oneMapsEqual(av, bv)
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !oneValuesEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}
