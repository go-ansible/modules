package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePkg5Publisher implements Ansible's `pkg5_publisher` module:
// manages which repository (a "publisher" in IPS terms) a Solaris
// 11+ client downloads `pkg5` packages from, via `pkg set-publisher`/
// `pkg unset-publisher`.
//
// Args: name (string, required; alias publisher); state (present|
// absent, default "present"); sticky (bool, optional — tri-state:
// omitted means "leave alone") — packages from a sticky repository can
// only receive updates from it; enabled (bool, optional — tri-state);
// origin ([]string, optional) — repository URL(s)/path(s); mirror
// ([]string, optional) — mirror URL(s)/path(s).
//
// Existing publishers are read via `pkg publisher -Ftsv` (a
// publisher/type/status/uri/sticky/... tab-separated table, one row
// per origin/mirror URI), matching real pkg5_publisher's own
// get_publishers() parse exactly. state=present: if name is a new
// publisher, or any GIVEN (non-nil) argument among origin/mirror/
// sticky/enabled differs from its current value, `pkg set-publisher`
// is run with `--remove-origin=* --add-origin=<u>...` (when origin is
// given — even an empty list, which clears every origin, matching
// real pkg5_publisher exactly), the same for mirror, plus `--sticky`/
// `--non-sticky` and `--enable`/`--disable` when those are given;
// state=absent: `pkg unset-publisher <name>` if it currently exists.
//
// Simplification vs real pkg5_publisher: check_mode is not modeled
// (see zfs_delegate_admin.go's own doc comment).
func modulePkg5Publisher(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "name", argString(args, "publisher", ""))
	if name == "" {
		return Result{}, errArg("pkg5_publisher: missing required argument: name (or its alias publisher)")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("pkg5_publisher: state must be present or absent, got %q", state)
	}

	existing, err := pkg5Publishers(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	cur, present := existing[name]

	if state == "absent" {
		if !present {
			return Ok(name + " already absent"), nil
		}
		if _, err := run(ctx, conn, "pkg unset-publisher "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " removed"), nil
	}

	origin, originSet := pkg5PublisherListArg(args, "origin")
	mirror, mirrorSet := pkg5PublisherListArg(args, "mirror")
	sticky, stickySet := args["sticky"].(bool)
	enabled, enabledSet := args["enabled"].(bool)

	needsSet := !present
	if present {
		if originSet && !pkg5PublisherEqualList(origin, cur.origin) {
			needsSet = true
		}
		if mirrorSet && !pkg5PublisherEqualList(mirror, cur.mirror) {
			needsSet = true
		}
		if stickySet && (cur.sticky == nil || *cur.sticky != sticky) {
			needsSet = true
		}
		if enabledSet && (cur.enabled == nil || *cur.enabled != enabled) {
			needsSet = true
		}
	}
	if !needsSet {
		return Ok(name + " already up to date"), nil
	}

	cmd := "pkg set-publisher"
	if originSet {
		cmd += " --remove-origin=*"
		for _, u := range origin {
			cmd += " --add-origin=" + shellQuote(u)
		}
	}
	if mirrorSet {
		cmd += " --remove-mirror=*"
		for _, u := range mirror {
			cmd += " --add-mirror=" + shellQuote(u)
		}
	}
	if stickySet {
		if sticky {
			cmd += " --sticky"
		} else {
			cmd += " --non-sticky"
		}
	}
	if enabledSet {
		if enabled {
			cmd += " --enable"
		} else {
			cmd += " --disable"
		}
	}
	cmd += " " + shellQuote(name)
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed(name + " updated"), nil
}

func pkg5PublisherListArg(args map[string]any, key string) ([]string, bool) {
	if _, ok := args[key]; !ok {
		return nil, false
	}
	return argStringList(args, key), true
}

func pkg5PublisherEqualList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type pkg5PublisherInfo struct {
	origin, mirror []string
	sticky         *bool
	enabled        *bool
}

// pkg5Publishers parses `pkg publisher -Ftsv`'s tab-separated table
// into one pkg5PublisherInfo per publisher name, matching real
// pkg5_publisher's own get_publishers()/unstringify() exactly: a "-"
// or empty field is nil/omitted, "true"/"false" become bool, and each
// origin/mirror row's uri is appended to that publisher's origin/
// mirror list in table order.
func pkg5Publishers(ctx context.Context, conn remoteexec.Connection) (map[string]pkg5PublisherInfo, error) {
	out, err := run(ctx, conn, "pkg publisher -Ftsv")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(out, "\n")
	if len(lines) == 0 {
		return map[string]pkg5PublisherInfo{}, nil
	}
	header := strings.Split(strings.ToLower(lines[0]), "\t")
	result := map[string]pkg5PublisherInfo{}
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		row := map[string]string{}
		for i, h := range header {
			if i < len(fields) {
				row[h] = fields[i]
			}
		}
		name := row["publisher"]
		info, ok := result[name]
		if !ok {
			info = pkg5PublisherInfo{}
		}
		if v, ok := pkg5Unstringify(row["sticky"]); ok {
			b := v == "true"
			info.sticky = &b
		}
		if v, ok := pkg5Unstringify(row["enabled"]); ok {
			b := v == "true"
			info.enabled = &b
		}
		if uri, ok := pkg5Unstringify(row["uri"]); ok {
			switch row["type"] {
			case "origin":
				info.origin = append(info.origin, uri)
			case "mirror":
				info.mirror = append(info.mirror, uri)
			}
		}
		result[name] = info
	}
	return result, nil
}

// pkg5Unstringify returns (value, true) unless value is "-" or "",
// which real pkg5_publisher's own unstringify() treats as absent
// (Python None).
func pkg5Unstringify(s string) (string, bool) {
	if s == "-" || s == "" {
		return "", false
	}
	return s, true
}
