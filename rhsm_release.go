package modules

import (
	"context"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// rhsmReleaseMatcher matches release-like values such as 7.2, 5.10,
// 6Server, 8, but rejects unlikely values like 100Server, 1.100,
// 7server — an exact port of real rhsm_release's own
// `release_matcher` regex.
var rhsmReleaseMatcher = regexp.MustCompile(`\b\d{1,2}(?:\.\d{1,2}|Server|Client|Workstation|)\b`)

// moduleRhsmRelease implements Ansible's `rhsm_release`
// (community.general) module: sets or unsets the RHEL minor release
// version RHSM repositories are pinned to, via `subscription-manager
// release`.
//
// Args: release (string, optional — unset, or an empty value, unsets
// the pin; a non-empty value must look like a release per
// rhsmReleaseMatcher above, matching real rhsm_release's own sanity
// check).
//
// Requires root (checked via `id -u` on the target, matching real
// rhsm_release's own `os.getuid() != 0` check — this port has no
// equivalent of Python's os.getuid() for a REMOTE target, so it runs
// `id -u` there instead, the same substitution dconf.go/puppet.go
// already use in this package for the same check). Fails cleanly
// (Result{Failed:true}) on an unregistered system, matching real
// rhsm_release's own behavior of letting `subscription-manager
// release --show`'s own non-zero exit surface as a failure — this
// port does not attempt to distinguish "unregistered" from any other
// subscription-manager error, same as real rhsm_release.
func moduleRhsmRelease(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	uid, err := run(ctx, conn, "id -u")
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(uid) != "0" {
		return Fail("rhsm_release: interacting with subscription-manager requires root permissions ('become: true')"), nil
	}

	targetRelease := argString(args, "release", "")
	if targetRelease != "" && !rhsmReleaseMatcher.MatchString(targetRelease) {
		return Fail("rhsm_release: \"" + targetRelease + "\" does not appear to be a valid release."), nil
	}

	res, err := runStatus(ctx, conn, "subscription-manager release --show")
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("rhsm_release: " + strings.TrimSpace(res.Stderr)), nil
	}
	currentRelease := rhsmReleaseMatcher.FindString(res.Stdout)

	changed := targetRelease != currentRelease
	if changed {
		var setCmd string
		if targetRelease == "" {
			setCmd = "subscription-manager release --unset"
		} else {
			setCmd = "subscription-manager release --set " + shellQuote(targetRelease)
		}
		setRes, err := runStatus(ctx, conn, setCmd)
		if err != nil {
			return Result{}, err
		}
		if setRes.RC != 0 {
			return Fail("rhsm_release: " + strings.TrimSpace(setRes.Stderr)), nil
		}
		currentRelease = targetRelease
	}

	result := Ok("release unchanged")
	if changed {
		result = Changed("release set to " + currentRelease)
	}
	return result.WithExtra("current_release", currentRelease), nil
}
