package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGioMime implements Ansible's `gio_mime` (community.general)
// module: sets the default handler for a MIME type via `gio mime` —
// read from real gio_mime.py's own module_utils/_gio_mime.py (this
// batch's hard rule: the exact `gio mime` invocations and output
// parsing are only visible there, not in EXAMPLES/RETURN VALUES).
//
// Args: mime_type (string, required); handler (string, required).
//
// Idempotency: reads the current default via `gio mime <mime_type>`
// (no handler argument queries rather than sets); real gio_mime_get's
// own output_process takes the FIRST line of stdout, splits it on
// whitespace, and keeps the LAST field as the handler — reproduced
// here identically. If stderr starts with "No default applications
// for" (gio's own wording when nothing is registered), real gio_mime
// treats that as "no handler set" (None) rather than an error; this
// port does the same string-prefix check. A handler differing from
// what's currently set (including the no-handler case) triggers
// `gio mime <mime_type> <handler>` to set it.
//
// version: `gio --version`'s trimmed output, always returned in Extra,
// matching real gio_mime's own always-populated `version` return value
// (added in community.general 10.0.0).
//
// Simplifications vs real gio_mime: no diff_mode output (this port has
// no per-field diff machinery, see blockinfile.go's own simplifications
// list for the same narrowing elsewhere in this package).
func moduleGioMime(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	mimeType, err := requireString(args, "mime_type")
	if err != nil {
		return Result{}, err
	}
	handler, err := requireString(args, "handler")
	if err != nil {
		return Result{}, err
	}

	if _, err := run(ctx, conn, "command -v gio"); err != nil {
		return Fail("gio_mime: gio executable not found on the target"), nil
	}

	version, err := run(ctx, conn, "gio --version")
	if err != nil {
		return Result{}, err
	}

	current, err := gioMimeGet(ctx, conn, mimeType)
	if err != nil {
		return Result{}, err
	}

	result := Ok("").WithExtra("handler", handler).WithExtra("version", version)
	if current == handler {
		return result, nil
	}

	res, err := runStatus(ctx, conn, "gio mime "+shellQuote(mimeType)+" "+shellQuote(handler))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("gio_mime: "+strings.TrimSpace(res.Stderr)).WithExtra("version", version), nil
	}
	return Changed("").WithExtra("handler", handler).WithExtra("version", version), nil
}

// gioMimeGet runs `gio mime <mimeType>` (no handler argument, which
// queries rather than sets) and returns the currently registered
// default handler, or "" if none is set — see moduleGioMime's own doc
// comment for the exact parsing/error-prefix rules this mirrors from
// real gio_mime_get.
func gioMimeGet(ctx context.Context, conn remoteexec.Connection, mimeType string) (string, error) {
	res, err := runStatus(ctx, conn, "gio mime "+shellQuote(mimeType))
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(res.Stderr, "No default applications for") {
		return "", nil
	}
	lines := strings.Split(res.Stdout, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return "", nil
	}
	fields := strings.Fields(lines[0])
	if len(fields) == 0 {
		return "", nil
	}
	return fields[len(fields)-1], nil
}
