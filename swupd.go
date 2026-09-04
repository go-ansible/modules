package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSwupd implements Ansible's `swupd` (community.general) module:
// manages OS updates and "bundles" via Clear Linux's own `swupd`
// bundle manager. Real swupd's own module was deprecated upstream in
// community.general (ClearLinux went EOL in July 2025, removal
// scheduled for community.general 15.0.0); this port still implements
// it as assigned since real ansible-doc still documents its full
// behavior.
//
// Args: name (string, alias bundle) — the bundle to install/remove;
// state ("present"|"absent", default "present"); update (bool,
// default false) — update the OS to the latest version (`swupd
// update`); verify (bool, default false) — verify (and, if inconsistent,
// `--fix`) the filesystem against the current or `manifest` version;
// exactly one of name/update/verify is required, matching real
// swupd's own required_one_of+mutually_exclusive over that same triple;
// format (string); manifest (int, aliases release/version); url
// (string) — overrides both contenturl and versionurl; contenturl,
// versionurl (string).
//
// Idempotency: bundle presence is read from whether
// `/usr/share/clear/bundles/<bundle>` exists on the target (matching
// real swupd's own _is_bundle_installed(), a local os.stat since real
// swupd's own module runs on the target); `update` is skipped when
// `swupd check-update` exits 1 (no update available; exit 0 means one
// is, any other exit code is a hard failure, matching real swupd's own
// _needs_update()); `verify` is skipped when `swupd verify`'s own
// stdout does not contain "files did not match".
//
// Real swupd's own module carries a few quirks this port reproduces
// verbatim, since they are what real swupd's own module actually
// does/says, not this port's own choices:
//   - real swupd's own _get_cmd() means to skip appending
//     `--contenturl=` for the `check-update` sub-command specifically,
//     but its own condition (`command != "check-update"`, where
//     `command` is always a Python list, never equal to a string) is
//     always true — so `--contenturl` is in practice appended to EVERY
//     sub-command whenever `contenturl` is set, `check-update`
//     included. This port matches that (always appends `--contenturl`
//     when set, url unset) rather than the evidently-intended
//     "skip it for check-update" behavior.
//   - remove_bundle()'s own "not installed" message is the literal
//     string "Bundle %s not installed" — an f-string that was written
//     without the leading `f`, so `%s` is never substituted with the
//     bundle name in real swupd's own output either.
//   - verify_os()'s own "no changes needed" message is "No files where
//     changed" (sic).
//   - a failed `swupd update` (after `check-update` already reported
//     one pending) reports the SAME failure message real swupd's own
//     module uses for a failed `check-update` ("Failed to check for
//     updates") — real swupd's own code reuses that string rather than
//     a message about the update itself having failed.
func moduleSwupd(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "name", argString(args, "bundle", ""))
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("swupd: state must be present or absent, got %q", state)
	}
	update := argBool(args, "update", false)
	verify := argBool(args, "verify", false)
	format := argString(args, "format", "")
	manifest := argInt(args, "manifest", argInt(args, "release", argInt(args, "version", 0)))
	url := argString(args, "url", "")
	contenturl := argString(args, "contenturl", "")
	versionurl := argString(args, "versionurl", "")

	set := 0
	if name != "" {
		set++
	}
	if update {
		set++
	}
	if verify {
		set++
	}
	if set == 0 {
		return Result{}, errArg("swupd: one of name, update, or verify is required")
	}
	if set > 1 {
		return Result{}, errArg("swupd: name, update, and verify are mutually exclusive")
	}

	switch {
	case update:
		return swupdUpdateOS(ctx, conn, format, manifest, url, contenturl, versionurl)
	case verify:
		return swupdVerifyOS(ctx, conn, format, manifest, url, contenturl, versionurl)
	case state == "present":
		return swupdInstallBundle(ctx, conn, name, format, manifest, url, contenturl, versionurl)
	default:
		return swupdRemoveBundle(ctx, conn, name, format, manifest, url, contenturl, versionurl)
	}
}

// swupdBaseArgs builds the `--format=`/`--manifest=`/`--url=`-or-
// `--contenturl=`+`--versionurl=` tail shared by every swupd
// sub-command, matching real swupd's own _get_cmd() (contenturl bug
// and all — see the package doc comment above).
func swupdBaseArgs(format string, manifest int, url, contenturl, versionurl string) []string {
	var out []string
	if format != "" {
		out = append(out, "--format="+format)
	}
	if manifest != 0 {
		out = append(out, fmt.Sprintf("--manifest=%d", manifest))
	}
	if url != "" {
		out = append(out, "--url="+url)
	} else {
		if contenturl != "" {
			out = append(out, "--contenturl="+contenturl)
		}
		if versionurl != "" {
			out = append(out, "--versionurl="+versionurl)
		}
	}
	return out
}

func swupdCmd(tokens []string, format string, manifest int, url, contenturl, versionurl string) string {
	all := append([]string{"swupd"}, tokens...)
	all = append(all, swupdBaseArgs(format, manifest, url, contenturl, versionurl)...)
	return quoteAll(all)
}

func swupdBundleInstalled(ctx context.Context, conn remoteexec.Connection, bundle string) (bool, error) {
	return pathExists(ctx, conn, "/usr/share/clear/bundles/"+bundle)
}

func swupdInstallBundle(ctx context.Context, conn remoteexec.Connection, bundle, format string, manifest int, url, contenturl, versionurl string) (Result, error) {
	installed, err := swupdBundleInstalled(ctx, conn, bundle)
	if err != nil {
		return Result{}, err
	}
	if installed {
		return Ok(fmt.Sprintf("Bundle %s is already installed", bundle)), nil
	}
	cmd := swupdCmd([]string{"bundle-add", bundle}, format, manifest, url, contenturl, versionurl)
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	r := Result{Extra: map[string]any{}}
	r = r.WithExtra("stdout", res.Stdout).WithExtra("stderr", res.Stderr)
	if res.RC == 0 {
		r.Changed = true
		r.Msg = fmt.Sprintf("Bundle %s installed", bundle)
		return r, nil
	}
	r.Failed = true
	r.Msg = fmt.Sprintf("Failed to install bundle %s", bundle)
	return r, nil
}

func swupdRemoveBundle(ctx context.Context, conn remoteexec.Connection, bundle, format string, manifest int, url, contenturl, versionurl string) (Result, error) {
	installed, err := swupdBundleInstalled(ctx, conn, bundle)
	if err != nil {
		return Result{}, err
	}
	if !installed {
		// Verbatim real swupd's own message — see the package doc
		// comment: it is written as a plain string, never
		// interpolated with the bundle name.
		return Ok("Bundle %s not installed"), nil
	}
	cmd := swupdCmd([]string{"bundle-remove", bundle}, format, manifest, url, contenturl, versionurl)
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	r := Result{Extra: map[string]any{}}
	r = r.WithExtra("stdout", res.Stdout).WithExtra("stderr", res.Stderr)
	if res.RC == 0 {
		r.Changed = true
		r.Msg = fmt.Sprintf("Bundle %s removed", bundle)
		return r, nil
	}
	r.Failed = true
	r.Msg = fmt.Sprintf("Failed to remove bundle %s", bundle)
	return r, nil
}

func swupdUpdateOS(ctx context.Context, conn remoteexec.Connection, format string, manifest int, url, contenturl, versionurl string) (Result, error) {
	checkCmd := swupdCmd([]string{"check-update"}, format, manifest, url, contenturl, versionurl)
	checkRes, err := runStatus(ctx, conn, checkCmd)
	if err != nil {
		return Result{}, err
	}
	switch checkRes.RC {
	case 1:
		return Ok("There are no updates available"), nil
	case 0:
		// needs update, fall through
	default:
		return Fail("Failed to check for updates").WithExtra("stdout", checkRes.Stdout).WithExtra("stderr", checkRes.Stderr), nil
	}

	cmd := swupdCmd([]string{"update"}, format, manifest, url, contenturl, versionurl)
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC == 0 {
		return Changed("Update successful").WithExtra("stdout", res.Stdout).WithExtra("stderr", res.Stderr), nil
	}
	// Real swupd's own module reuses check-update's own failure
	// message here too — see the package doc comment.
	return Fail("Failed to check for updates").WithExtra("stdout", res.Stdout).WithExtra("stderr", res.Stderr), nil
}

func swupdVerifyOS(ctx context.Context, conn remoteexec.Connection, format string, manifest int, url, contenturl, versionurl string) (Result, error) {
	cmd := swupdCmd([]string{"verify"}, format, manifest, url, contenturl, versionurl)
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("Failed to check for filesystem inconsistencies.").WithExtra("stdout", res.Stdout).WithExtra("stderr", res.Stderr), nil
	}
	if !strings.Contains(res.Stdout, "files did not match") {
		return Ok("No files where changed"), nil
	}

	fixCmd := swupdCmd([]string{"verify", "--fix"}, format, manifest, url, contenturl, versionurl)
	fixRes, err := runStatus(ctx, conn, fixCmd)
	if err != nil {
		return Result{}, err
	}
	fixed := strings.Contains(fixRes.Stdout, "missing files were replaced") ||
		strings.Contains(fixRes.Stdout, "files were fixed") ||
		strings.Contains(fixRes.Stdout, "files were deleted")
	if fixRes.RC == 0 && fixed {
		return Changed("Fix successful").WithExtra("stdout", fixRes.Stdout).WithExtra("stderr", fixRes.Stderr), nil
	}
	return Fail("Failed to verify the OS").WithExtra("stdout", fixRes.Stdout).WithExtra("stderr", fixRes.Stderr), nil
}
