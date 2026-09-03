package modules

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// androidSdkChannels maps real android_sdk's own channel names to the
// numeric --channel=N value sdkmanager itself expects (see real
// android_sdk's module_utils/_android_sdkmanager.py __channel_map).
var androidSdkChannels = map[string]int{
	"stable": 0,
	"beta":   1,
	"dev":    2,
	"canary": 3,
}

var (
	androidSdkInstalledHeaderRe = regexp.MustCompile(`^Installed packages:$`)
	androidSdkInstalledLineRe   = regexp.MustCompile(`^\s*(\S+)\s*\|\s*[0-9][^|]*\b\s*\|\s*.+\s*\|\s*(\S+)\s*$`)
	androidSdkUpdatableHeaderRe = regexp.MustCompile(`^Available Updates:$`)
	androidSdkUpdatableLineRe   = regexp.MustCompile(`^\s*(\S+)\s*\|\s*[0-9][^|]*\b\s*\|\s*[0-9].*\b\s*$`)
)

// moduleAndroidSdk implements Ansible's `android_sdk` (community.general)
// module: installs, removes, or updates Android SDK packages via the
// `sdkmanager` command-line tool.
//
// Args: name (string or []string, required — aliases package/pkg; the
// same package name may not repeat, matching real android_sdk's own
// fail_json check); state (present|absent|latest, default "present");
// sdk_root (string, optional) — passed as sdkmanager's own
// `--sdk_root=`; channel (stable|beta|dev|canary, default "stable") —
// mapped to sdkmanager's own numeric `--channel=N` (0-3), exactly
// matching real android_sdk's own __channel_map; accept_licenses (bool,
// default false) — answers "y" (else "N") on stdin to sdkmanager's
// interactive per-package license prompt.
//
// State semantics, matching real android_sdk's own module_utils
// (_android_sdkmanager.py) faithfully, including one real quirk:
//   - present: installs every named package not already reported by
//     `sdkmanager --list_installed`.
//   - absent: uninstalls every named package that IS reported installed.
//   - latest: installs every named package not already installed, UNION
//     every package `sdkmanager --list --newer` reports system-wide as
//     updatable. That union is NOT filtered down to the packages named
//     in this task: real android_sdk's own state_latest() unions the
//     WHOLE machine's updatable-package set into the install set
//     unconditionally, so state=latest naming one unrelated package can
//     still upgrade every other outdated package already on the SDK.
//     This port replicates that faithfully — it is real android_sdk's
//     own shipped behavior, not a bug introduced by this port.
//
// Because only one license prompt can be answered per sdkmanager
// invocation (see real android_sdk's own NOTES on this), each package
// is installed/uninstalled with its own separate `sdkmanager`
// invocation, exactly matching that same real constraint.
//
// Simplifications vs real android_sdk: check_mode is not modeled (see
// zfs_delegate_admin.go's own doc comment for this port's general
// convention there); `installed`/`removed` Extra fields report exactly
// the package names this port SENT to sdkmanager (as real android_sdk's
// own vars.installed/vars.removed do before check_mode short-circuits),
// not a per-package parse of sdkmanager's own success/failure output.
func moduleAndroidSdk(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names, err := androidSdkNames(args)
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	sdkRoot := argString(args, "sdk_root", "")
	channel := argString(args, "channel", "stable")
	channelNum, ok := androidSdkChannels[channel]
	if !ok {
		return Result{}, errArg("android_sdk: channel must be one of stable, beta, dev, canary, got %q", channel)
	}
	acceptLicenses := argBool(args, "accept_licenses", false)

	suffix := ""
	if sdkRoot != "" {
		suffix += " --sdk_root=" + shellQuote(sdkRoot)
	}
	suffix += fmt.Sprintf(" --channel=%d", channelNum)

	installed, err := androidSdkQuery(ctx, conn, "sdkmanager --list_installed"+suffix,
		androidSdkInstalledHeaderRe, androidSdkInstalledLineRe)
	if err != nil {
		return Result{}, err
	}

	switch state {
	case "present":
		var toInstall []string
		for _, n := range names {
			if !sliceHasString(installed, n) {
				toInstall = append(toInstall, n)
			}
		}
		if len(toInstall) == 0 {
			return Ok("already installed"), nil
		}
		if err := androidSdkApply(ctx, conn, "--install", toInstall, suffix, acceptLicenses); err != nil {
			return Result{}, err
		}
		return Changed(strings.Join(toInstall, ", ")).WithExtra("installed", toInstall).WithExtra("removed", []string{}), nil

	case "absent":
		var toRemove []string
		for _, n := range names {
			if sliceHasString(installed, n) {
				toRemove = append(toRemove, n)
			}
		}
		if len(toRemove) == 0 {
			return Ok("already absent"), nil
		}
		if err := androidSdkApply(ctx, conn, "--uninstall", toRemove, suffix, false); err != nil {
			return Result{}, err
		}
		return Changed(strings.Join(toRemove, ", ")).WithExtra("installed", []string{}).WithExtra("removed", toRemove), nil

	case "latest":
		updatable, err := androidSdkQuery(ctx, conn, "sdkmanager --list --newer"+suffix,
			androidSdkUpdatableHeaderRe, androidSdkUpdatableLineRe)
		if err != nil {
			return Result{}, err
		}
		toInstallSet := map[string]bool{}
		for _, n := range names {
			if !sliceHasString(installed, n) {
				toInstallSet[n] = true
			}
		}
		for _, n := range updatable {
			toInstallSet[n] = true
		}
		if len(toInstallSet) == 0 {
			return Ok("already up to date"), nil
		}
		toInstall := make([]string, 0, len(toInstallSet))
		for n := range toInstallSet {
			toInstall = append(toInstall, n)
		}
		sort.Strings(toInstall)
		if err := androidSdkApply(ctx, conn, "--install", toInstall, suffix, acceptLicenses); err != nil {
			return Result{}, err
		}
		return Changed(strings.Join(toInstall, ", ")).WithExtra("installed", toInstall).WithExtra("removed", []string{}), nil

	default:
		return Result{}, errArg("android_sdk: state must be present, absent, or latest, got %q", state)
	}
}

// androidSdkNames reads the name/package/pkg argument (an alias trio in
// real android_sdk) as a string list and rejects a repeated entry.
func androidSdkNames(args map[string]any) ([]string, error) {
	names := argStringList(args, "name")
	if len(names) == 0 {
		names = argStringList(args, "package")
	}
	if len(names) == 0 {
		names = argStringList(args, "pkg")
	}
	if len(names) == 0 {
		return nil, errArg("android_sdk: missing required argument: name")
	}
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		if seen[n] {
			return nil, errArg("android_sdk: packages may not repeat: %q", n)
		}
		seen[n] = true
	}
	return names, nil
}

// androidSdkQuery runs a sdkmanager listing command and extracts every
// package name from the rows following headerRe, matching lineRe —
// exactly mirroring real android_sdk's own _parse_packages() (see its
// own _RE_INSTALLED_PACKAGE / _RE_UPDATABLE_PACKAGE row regexes).
func androidSdkQuery(ctx context.Context, conn remoteexec.Connection, cmd string, headerRe, lineRe *regexp.Regexp) ([]string, error) {
	out, err := run(ctx, conn, cmd)
	if err != nil {
		return nil, fmt.Errorf("android_sdk: %w", err)
	}
	var names []string
	sectionFound := false
	for _, line := range strings.Split(out, "\n") {
		if !sectionFound {
			sectionFound = headerRe.MatchString(line)
			continue
		}
		if m := lineRe.FindStringSubmatch(line); m != nil {
			names = append(names, m[1])
		}
	}
	return names, nil
}

// androidSdkApply installs (verb "--install") or uninstalls (verb
// "--uninstall") every named package with its own sdkmanager
// invocation, feeding the license prompt answer on stdin.
func androidSdkApply(ctx context.Context, conn remoteexec.Connection, verb string, names []string, suffix string, acceptLicenses bool) error {
	answer := "N\n"
	if acceptLicenses {
		answer = "y\n"
	}
	for _, name := range names {
		cmd := "sdkmanager " + verb + " " + shellQuote(name) + suffix
		res, err := conn.Exec(ctx, cmd, strings.NewReader(answer))
		if err != nil {
			return fmt.Errorf("android_sdk: %w", err)
		}
		if res.RC != 0 {
			return fmt.Errorf("android_sdk: sdkmanager %s %s failed: %s", verb, name, strings.TrimSpace(res.Stderr))
		}
	}
	return nil
}

func sliceHasString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
