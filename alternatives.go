package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAlternatives implements (a subset of) Ansible's `alternatives`
// module: manages a symlink group via the `update-alternatives` tool
// (Debian-family), falling back to the RHEL-family tool's own name,
// `alternatives`, the same hostname.go-style two-tier strategy this
// package already uses for `hostname` — real alternatives is
// documented as available on both families, and the executable itself
// (not just its options) differs between them, which the task
// description for this batch specifically flagged rather than letting
// this port silently assume Debian.
//
// Args: name (string, required); path (string, required for
// present/selected/auto — the real executable an alternative should
// point to); link (string, optional — the symlink path; required by
// real alternatives on RHEL, or on Debian when name is not yet known to
// the system; this port always passes it through when given, but does
// not itself enforce real alternatives' own RHEL-vs-Debian requiredness
// rules); priority (int, default 50); state
// (present|selected|auto|absent, default "selected"); family (string,
// optional, RHEL-only real alternatives concept) — forwarded as
// `--family` when given; subcommands ([]map, optional, aliased
// `slaves`) — each {name,link,path} becomes one `--slave <link> <name>
// <path>` triple appended to the install invocation, real
// update-alternatives syntax.
//
// Idempotency: modeled on apt_key.go's own best-effort
// grep-over-a-listing-command pattern. "Is name installed pointing at
// path" is checked via `<tool> --display <name>` output containing
// path as a substring; "is path currently selected" and "is the group
// currently in auto mode" are checked via `<tool> --query <name>`
// output's own documented "Value:"/"Status:" lines. Both are textual
// heuristics over a tool whose exact output format varies by
// implementation/version — the same tradeoff apt_key.go and debconf.go
// already accept for the same underlying reason (no portable structured
// output from the tool itself).
//
// Simplifications vs real alternatives: no diff_mode support beyond
// what Result already offers.
func moduleAlternatives(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "selected")
	link := argString(args, "link", "")
	family := argString(args, "family", "")

	if state == "absent" {
		installed, err := alternativesInstalled(ctx, conn, name, "")
		if err != nil {
			return Result{}, err
		}
		if !installed {
			return Ok(name + " already absent"), nil
		}
		path := argString(args, "path", "")
		if path == "" {
			// real update-alternatives --remove needs both name and
			// path; if the caller didn't give one, fall back to
			// whichever path is currently selected.
			path, err = alternativesQueryField(ctx, conn, name, "Value")
			if err != nil {
				return Result{}, err
			}
			if path == "" {
				return Result{}, errArg("alternatives: state=absent could not determine which path to remove for %q; pass path explicitly", name)
			}
		}
		cmd := alternativesToolPrefix() + "$T --remove " + shellQuote(name) + " " + shellQuote(path)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(name + " removed"), nil
	}

	path, err := requireString(args, "path")
	if err != nil {
		return Result{}, errArg("alternatives: path is required when state is %s", state)
	}
	priority := argInt(args, "priority", 50)

	changed := false
	installed, err := alternativesInstalled(ctx, conn, name, path)
	if err != nil {
		return Result{}, err
	}
	if !installed {
		cmd := alternativesInstallCmd(name, link, path, priority, family, alternativesSubcommands(args))
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		changed = true
	}

	switch state {
	case "present":
		// installed only, done above.

	case "selected":
		current, err := alternativesQueryField(ctx, conn, name, "Value")
		if err != nil {
			return Result{}, err
		}
		if current != path {
			cmd := alternativesToolPrefix() + "$T --set " + shellQuote(name) + " " + shellQuote(path)
			if _, err := run(ctx, conn, cmd); err != nil {
				return Result{}, err
			}
			changed = true
		}

	case "auto":
		status, err := alternativesQueryField(ctx, conn, name, "Status")
		if err != nil {
			return Result{}, err
		}
		if status != "auto" {
			cmd := alternativesToolPrefix() + "$T --auto " + shellQuote(name)
			if _, err := run(ctx, conn, cmd); err != nil {
				return Result{}, err
			}
			changed = true
		}

	default:
		return Result{}, errArg("alternatives: state must be present, selected, auto, or absent, got %q", state)
	}

	if changed {
		return Changed(name), nil
	}
	return Ok(name), nil
}

// alternativesToolPrefix is a shell fragment that resolves $T to
// "update-alternatives" if available, else "alternatives" (RHEL's tool
// name) — see moduleAlternatives' doc comment.
func alternativesToolPrefix() string {
	return "if command -v update-alternatives >/dev/null 2>&1; then T=update-alternatives; else T=alternatives; fi; "
}

func alternativesSubcommands(args map[string]any) []map[string]any {
	v, ok := args["subcommands"]
	if !ok {
		v, ok = args["slaves"]
	}
	if !ok {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func alternativesInstallCmd(name, link, path string, priority int, family string, subcommands []map[string]any) string {
	var b strings.Builder
	b.WriteString(alternativesToolPrefix())
	b.WriteString("$T --install ")
	b.WriteString(shellQuote(link))
	b.WriteString(" ")
	b.WriteString(shellQuote(name))
	b.WriteString(" ")
	b.WriteString(shellQuote(path))
	b.WriteString(" ")
	b.WriteString(strconv.Itoa(priority))
	for _, sc := range subcommands {
		scName, _ := sc["name"].(string)
		scLink, _ := sc["link"].(string)
		scPath, _ := sc["path"].(string)
		b.WriteString(" --slave ")
		b.WriteString(shellQuote(scLink))
		b.WriteString(" ")
		b.WriteString(shellQuote(scName))
		b.WriteString(" ")
		b.WriteString(shellQuote(scPath))
	}
	if family != "" {
		b.WriteString(" --family ")
		b.WriteString(shellQuote(family))
	}
	return b.String()
}

// alternativesInstalled reports whether name is installed at all (path
// == "") or, when path is given, installed specifically pointing at
// path — via a substring grep over `--display`'s output, the same
// best-effort approach apt_key.go uses over `apt-key list`.
func alternativesInstalled(ctx context.Context, conn remoteexec.Connection, name, path string) (bool, error) {
	cmd := alternativesToolPrefix() + "$T --display " + shellQuote(name) + " 2>/dev/null"
	if path != "" {
		cmd += " | grep -qF " + shellQuote(path)
	} else {
		cmd += " >/dev/null"
	}
	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

// alternativesQueryField greps `--query <name>`'s output for a
// "<field>: <value>" line, returning value (or "" if not found/not
// installed yet).
func alternativesQueryField(ctx context.Context, conn remoteexec.Connection, name, field string) (string, error) {
	cmd := alternativesToolPrefix() + "$T --query " + shellQuote(name) + " 2>/dev/null | grep '^" + field + ": '"
	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", nil
	}
	line := strings.TrimSpace(strings.SplitN(res.Stdout, "\n", 2)[0])
	return strings.TrimSpace(strings.TrimPrefix(line, field+":")), nil
}
