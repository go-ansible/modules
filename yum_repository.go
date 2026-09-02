package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleYumRepository implements (a subset of) Ansible's
// `yum_repository` module: writes (or removes) a `.repo` INI-style
// file under /etc/yum.repos.d/ — the RPM-family counterpart of
// apt_repository.go's plain-line path.
//
// Args: name (string, required) — the repo's section id AND (when
// `file` is unset) its destination filename stem, matching real
// yum_repository's own default; file (string, optional, default =
// name); description (string, required when state=present — real
// yum_repository requires it too, since it becomes the repo's `name=`
// INI field); baseurl ([]string, required when state=present — this
// port does not support metalink/mirrorlist as alternatives, only
// baseurl); gpgcheck (bool, optional); gpgkey ([]string, optional);
// enabled (bool, optional); state (present|absent, default "present").
//
// Simplifications vs real yum_repository: none of async, bandwidth,
// cost, countme, exclude, proxy, throttle, or the several dozen other
// yum.conf-section keys real yum_repository exposes are supported —
// only the handful written above. Multi-value fields (baseurl, gpgkey)
// are written one value per line with NO leading whitespace on
// continuation lines; real yum_repository (and yum.conf's own INI
// dialect) indents continuation lines to mark them as part of the
// previous key — omitting that indentation is invalid multi-value INI
// syntax for a real yum.conf parser when more than one baseurl/gpgkey
// is given, a real, documented gap versus real yum_repository's exact
// output.
//
// state=absent removes the ENTIRE destination file, not just this
// repo's `[name]` section — a deviation that only matters when
// multiple repos share one `file` (real yum_repository can add/remove
// one repo's section while leaving sibling sections in the same file
// untouched; this port cannot, since it never parses the file's
// existing sections, only ever overwrites or deletes it whole).
//
// Idempotency for state=present is checked by comparing the
// destination file's existing content against the wanted stanza
// byte-for-byte, the same pattern apt_repository.go and
// deb822_repository.go use.
func moduleYumRepository(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("yum_repository: state must be present or absent, got %q", state)
	}
	file := argString(args, "file", name)
	path := "/etc/yum.repos.d/" + file + ".repo"

	if state == "absent" {
		exists, err := pathExists(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Ok(path + " already absent"), nil
		}
		if _, err := run(ctx, conn, "rm -f "+shellQuote(path)); err != nil {
			return Result{}, err
		}
		return Changed(path + " removed"), nil
	}

	description, err := requireString(args, "description")
	if err != nil {
		return Result{}, errArg("yum_repository: description is required when state is present")
	}
	baseurl := argStringList(args, "baseurl")
	if len(baseurl) == 0 {
		return Result{}, errArg("yum_repository: baseurl is required when state is present")
	}
	gpgkey := argStringList(args, "gpgkey")

	var gpgcheck, enabled *bool
	if _, ok := args["gpgcheck"]; ok {
		b := argBool(args, "gpgcheck", false)
		gpgcheck = &b
	}
	if _, ok := args["enabled"]; ok {
		b := argBool(args, "enabled", false)
		enabled = &b
	}

	stanza := yumRepoStanza(name, description, baseurl, gpgcheck, gpgkey, enabled)

	exists, err := pathExists(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}
	if exists {
		current, err := run(ctx, conn, "cat "+shellQuote(path))
		if err != nil {
			return Result{}, err
		}
		if current == strings.TrimRight(stanza, "\n") {
			return Ok(path + " unchanged"), nil
		}
	}

	cmd := "mkdir -p /etc/yum.repos.d && printf '%s' " + shellQuote(stanza) + " > " + shellQuote(path)
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed(path), nil
}

// yumRepoStanza composes a single yum .repo INI section, separated out
// so its exact shape can be asserted directly in tests.
func yumRepoStanza(name, description string, baseurl []string, gpgcheck *bool, gpgkey []string, enabled *bool) string {
	var b strings.Builder
	b.WriteString("[" + name + "]\n")
	b.WriteString("name=" + description + "\n")
	b.WriteString("baseurl=" + strings.Join(baseurl, "\n") + "\n")
	if gpgcheck != nil {
		b.WriteString("gpgcheck=" + boolToYumFlag(*gpgcheck) + "\n")
	}
	if len(gpgkey) > 0 {
		b.WriteString("gpgkey=" + strings.Join(gpgkey, "\n") + "\n")
	}
	if enabled != nil {
		b.WriteString("enabled=" + boolToYumFlag(*enabled) + "\n")
	}
	return b.String()
}

func boolToYumFlag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
