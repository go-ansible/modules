package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleXdgMime implements Ansible's `xdg_mime` (community.general)
// module: sets the default handler for one or more MIME types via
// `xdg-mime` — read from real xdg_mime.py's own module_utils/
// _xdg_mime.py (this batch's hard rule: the per-mime-type query loop
// and single-call write are only visible there).
//
// Args: mime_types ([]string, required); handler (string, required) —
// must end in ".desktop", matching real xdg_mime's own validation;
// given anything else, this module fails cleanly (Result{Failed:true})
// rather than passing a malformed handler through to xdg-mime.
//
// Idempotency: for EACH mime type, runs `xdg-mime query default
// <mime_type>` (one call per type — real xdg_mime_get is invoked once
// per entry of mime_types, not batched, since `xdg-mime query default`
// itself only accepts one MIME type per invocation) and keeps the last
// whitespace-separated field of the first output line (empty output
// means "no handler set", matching real xdg_mime_get's own `if not
// out.strip(): return None`, represented here as ""). If ANY current
// handler differs from the given one, the whole set is written in ONE
// `xdg-mime default <handler> <mime_type...>` call (matching real
// xdg_mime's own `args_order="default handler mime_types"` with
// mime_types passed as a list — xdg-mime itself applies one handler to
// every MIME type given on that single command line).
//
// version: `xdg-mime --version`'s output with a leading "xdg-mime "
// prefix stripped (real xdg-mime prints e.g. "xdg-mime 1.2.1"),
// matching real xdg_mime's own parsing exactly; always returned in
// Extra.
//
// Real xdg_mime's own doc comment notes: "If the desktop file is not
// installed, the module does not fail, but the handler is not set
// either" — xdg-mime itself silently accepts an unknown .desktop name;
// this port does not special-case that, since it is xdg-mime's own
// behavior being passed through unchanged, not something this module
// needs to detect.
//
// Simplifications vs real xdg_mime: no diff_mode (real xdg_mime itself
// does not support it either, so this is not a narrowing versus
// upstream).
func moduleXdgMime(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	mimeTypes := argStringList(args, "mime_types")
	if len(mimeTypes) == 0 {
		return Result{}, errArg("xdg_mime: missing required argument: mime_types")
	}
	handler, err := requireString(args, "handler")
	if err != nil {
		return Result{}, err
	}
	if !strings.HasSuffix(handler, ".desktop") {
		return Fail("xdg_mime: handler must be a .desktop file"), nil
	}

	if _, err := run(ctx, conn, "command -v xdg-mime"); err != nil {
		return Fail("xdg_mime: xdg-mime executable not found on the target"), nil
	}

	versionOut, err := run(ctx, conn, "xdg-mime --version")
	if err != nil {
		return Result{}, err
	}
	version := strings.TrimSpace(strings.TrimPrefix(versionOut, "xdg-mime"))

	currentHandlers := make([]string, len(mimeTypes))
	anyDiff := false
	for i, mt := range mimeTypes {
		cur, err := xdgMimeGet(ctx, conn, mt)
		if err != nil {
			return Result{}, err
		}
		currentHandlers[i] = cur
		if cur != handler {
			anyDiff = true
		}
	}

	result := Ok("").WithExtra("current_handlers", currentHandlers).WithExtra("version", version)
	if !anyDiff {
		return result, nil
	}

	cmd := "xdg-mime default " + shellQuote(handler)
	for _, mt := range mimeTypes {
		cmd += " " + shellQuote(mt)
	}
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("xdg_mime: "+strings.TrimSpace(res.Stderr)).WithExtra("version", version), nil
	}
	return Changed("").WithExtra("current_handlers", currentHandlers).WithExtra("version", version), nil
}

// xdgMimeGet runs `xdg-mime query default <mimeType>` and returns the
// currently registered handler, or "" if none is set.
func xdgMimeGet(ctx context.Context, conn remoteexec.Connection, mimeType string) (string, error) {
	res, err := runStatus(ctx, conn, "xdg-mime query default "+shellQuote(mimeType))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(res.Stdout) == "" {
		return "", nil
	}
	lines := strings.Split(res.Stdout, "\n")
	fields := strings.Fields(lines[0])
	if len(fields) == 0 {
		return "", nil
	}
	return fields[len(fields)-1], nil
}
