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

	engine := gotemplate.New()
	rendered, err := engine.Render(string(raw), vars)
	if err != nil {
		return Result{}, fmt.Errorf("template: rendering %q: %w", src, err)
	}

	changed := false
	current, err := fetchIfExists(ctx, conn, dest)
	if err != nil {
		return Result{}, err
	}
	if current == nil || string(current) != rendered {
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
			if _, err := run(ctx, conn, fmt.Sprintf("chmod %04o %s", *mode, shellQuote(dest))); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	if changed {
		return Changed(dest), nil
	}
	return Ok(dest), nil
}
