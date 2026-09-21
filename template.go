package modules

import (
	"context"
	"fmt"
	"os"

	remoteexec "github.com/go-remoteexec/transport"

	gotemplate "github.com/go-ansible/template"
)

// moduleTemplate implements Ansible's `template` module: renders a
// local Jinja2 template file against the full variable context and
// writes the result to dest, idempotently.
//
// Args: src (string, required) — local template file path; dest
// (string, required); mode (octal string); _vars (map[string]any) —
// the full variable scope to render with, distinct from this module's
// own args (matching Ansible's template action plugin, which renders
// against the whole variable scope, not just its own arguments — the
// caller populates this key rather than letting normal per-arg
// templating handle it, since the FILE's content needs rendering, not
// an argument's string value).
func moduleTemplate(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	src, err := requireString(args, "src")
	if err != nil {
		return Result{}, err
	}
	dest, err := requireString(args, "dest")
	if err != nil {
		return Result{}, err
	}
	mode, err := argMode(args, "mode")
	if err != nil {
		return Result{}, err
	}
	vars, _ := args["_vars"].(map[string]any)
	if vars == nil {
		vars = map[string]any{}
	}

	raw, err := os.ReadFile(src)
	if err != nil {
		return Result{}, fmt.Errorf("template: reading src %q: %w", src, err)
	}

	// A .j2 file is rendered with plain JINJA2 string-literal
	// semantics, not the raw ones a playbook's own expressions use.
	// Real ansible-core 2.21 differs between the two, measured with one
	// expression in two places: `{{ "x\ny" | length }}` is 4 inline
	// and 3 in a file, and a file containing 'C:\Users' fails there
	// with Jinja2's "truncated \UXXXXXXXX escape".
	engine := gotemplate.New().JinjaStringEscapes()
	rendered, err := engine.Render(string(raw), vars)
	if err != nil {
		return Result{}, fmt.Errorf("template: rendering %q: %w", src, err)
	}

	// The template is still rendered in check mode — that is most of what
	// a dry run is for here, since a broken template should fail the
	// check rather than wait for the real run. Only the write is skipped.
	check := InCheckMode(args)

	changed := false
	// diff stays zero unless --diff asked for one; Result.WithDiff
	// treats a zero Diff as nothing to report.
	var diff Diff
	current, err := fetchIfExists(ctx, conn, dest)
	if err != nil {
		return Result{}, err
	}
	if current == nil || string(current) != rendered {
		if InDiffMode(args) {
			// Real Ansible renders to a temporary file and names that
			// file as the after side, e.g.
			// "~/.ansible/tmp/ansible-local-.../my.j2" — a path that
			// differs between two runs on the same machine, and that
			// this port has no equivalent of, since it renders in
			// memory. The dest path is used instead, matching what
			// copy with content: reports, and is the one divergence in
			// these headers.
			diff = ContentDiff(dest, dest, current, []byte(rendered))
		}
		if check {
			return Changed(dest).WithDiff(diff), nil
		}
		if err := writeRemote(ctx, conn, dest, []byte(rendered)); err != nil {
			return Result{}, err
		}
		changed = true
	}

	if mode != nil {
		info, err := statPath(ctx, conn, dest)
		if err != nil {
			return Result{}, err
		}
		if info == nil || info.mode != *mode {
			if check {
				return Changed(dest).WithDiff(diff), nil
			}
			if _, err := run(ctx, conn, fmt.Sprintf("chmod %04o %s", *mode, shellQuote(dest))); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	if changed {
		return Changed(dest).WithDiff(diff), nil
	}
	return Ok(dest), nil
}
