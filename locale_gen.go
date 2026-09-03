package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLocaleGen implements (a subset of) Ansible's `locale_gen`
// (community.general) module: ensures one or more locales are
// generated (or not) via the glibc `/etc/locale.gen` mechanism.
//
// Args: name ([]string, required — real locale_gen also accepts a bare
// string for a single locale before community.general 9.3.0; this port
// only accepts a list, since argStringList already normalizes a single
// string into a one-element list transparently); state (present|
// absent, default "present").
//
// Real locale_gen's own documented behavior is: "If /etc/locale.gen
// exists, the module assumes the glibc mechanism, else it raises an
// error" — support for the older /var/lib/locales/supported.d/
// (ubuntu_legacy) mechanism was removed upstream. That single check is
// exactly what already keeps this module correctly scoped to Debian/
// Ubuntu/Arch (all of which ship /etc/locale.gen) and cleanly rejects a
// RHEL-family target (which manages locales via `localedef` instead and
// has no /etc/locale.gen) — this port reproduces that same check rather
// than adding separate RHEL-specific logic, since the "does /etc/
// locale.gen exist" gate already gives RHEL a clear, named Fail instead
// of silently doing nothing or misbehaving.
//
// For each requested locale, the entry matched/written in
// /etc/locale.gen is "<locale> <charset>" where charset is whatever
// follows the first "." in the locale name (e.g. "de_CH.UTF-8" ->
// charset "UTF-8"); a locale with no "." is written as a bare line.
// state=present uncomments a matching commented-out template line if
// one exists, or appends a new active line if no line (commented or
// not) matches at all; state=absent comments out a matching active
// line. `locale-gen` is run once at the end if anything changed.
//
// Simplifications vs real locale_gen: no check against
// /usr/share/i18n/SUPPORTED for whether the requested locale is even
// buildable (real locale_gen asserts availability there before editing
// locale.gen; this port lets `locale-gen`'s own exit status be the
// final word, via `run`'s non-zero-exit-is-an-error convention) — an
// invalid locale name surfaces as a `locale-gen` command failure rather
// than a distinct earlier error message.
func moduleLocaleGen(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names := argStringList(args, "name")
	if len(names) == 0 {
		return Result{}, errArg("locale_gen: missing required argument: name")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("locale_gen: state must be present or absent, got %q", state)
	}

	const localeGenFile = "/etc/locale.gen"
	exists, err := pathExists(ctx, conn, localeGenFile)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Fail("locale_gen: " + localeGenFile + " does not exist on the target; this port only supports the glibc " +
			"locale.gen mechanism (Debian/Ubuntu/Arch) — a RHEL-family target manages locales via localedef instead, " +
			"which is not implemented (see moduleLocaleGen's doc comment)"), nil
	}

	res, err := runStatus(ctx, conn, "cat "+shellQuote(localeGenFile))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Result{}, errArg("locale_gen: reading %s: %s", localeGenFile, strings.TrimSpace(res.Stderr))
	}
	lines := splitLines(res.Stdout)

	changed := false
	for _, name := range names {
		entry := localeGenEntry(name)
		var entryChanged bool
		lines, entryChanged = localeGenApplyEntry(lines, entry, state)
		changed = changed || entryChanged
	}

	if !changed {
		return Ok("locales unchanged").WithExtra("mechanism", "glibc"), nil
	}

	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	if err := writeRemote(ctx, conn, localeGenFile, []byte(content)); err != nil {
		return Result{}, err
	}
	if _, err := run(ctx, conn, "locale-gen"); err != nil {
		return Result{}, err
	}
	return Changed(strings.Join(names, ", ")).WithExtra("mechanism", "glibc"), nil
}

func localeGenEntry(name string) string {
	if idx := strings.Index(name, "."); idx >= 0 {
		return name + " " + name[idx+1:]
	}
	return name
}

// localeGenApplyEntry finds the line (commented or not) whose text,
// stripped of a leading "# " comment marker, equals entry.
// state=present uncomments it (or appends an active line if no line at
// all matches); state=absent comments out an active match.
func localeGenApplyEntry(lines []string, entry, state string) ([]string, bool) {
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		isComment := strings.HasPrefix(trimmed, "#")
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		if body != entry {
			continue
		}
		switch state {
		case "present":
			if !isComment {
				return lines, false
			}
			lines[i] = entry
			return lines, true
		case "absent":
			if isComment {
				return lines, false
			}
			lines[i] = "# " + entry
			return lines, true
		}
	}
	if state == "present" {
		return append(lines, entry), true
	}
	return lines, false // absent, and no active (or any) matching line found
}
