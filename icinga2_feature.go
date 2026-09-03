package modules

import (
	"context"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIcinga2Feature implements Ansible's `icinga2_feature`
// (community.general) module: enables or disables an Icinga2 feature
// via `icinga2 feature enable|disable` — read from real
// icinga2_feature.py's own Icinga2FeatureHelper.manage (this batch's
// hard rule: the exact "already enabled"/"does not exist" wording this
// module keys off is only visible there, not EXAMPLES/OPTIONS).
//
// The `icinga2` CLI itself covers this whole module (unlike
// icinga2_host.go/icinga2_downtime.go, which fall back to curl against
// the Icinga2 REST API — see those files' own doc comments for why),
// matching real icinga2_feature's own module.get_bin_path("icinga2",
// True) plus subprocess calls exactly; this port only substitutes
// conn.Exec for module.run_command (see module.go's own doc comment on
// this package's architecture).
//
// Args: name (required); state (present|absent, default present).
//
// `icinga2 feature list` output is matched against
// "Disabled features:.* <name>[ \n]" / "Enabled features:.* <name>[
// \n]" (the same regexes real icinga2_feature uses, ported as-is,
// including the trailing-space-or-newline boundary that keeps a
// prefix-only name match from false-positiving, e.g. "api" not
// matching within "api-users"). Already in the desired state: no-op.
// Otherwise runs `icinga2 feature enable|disable <name>` and applies
// real icinga2_feature's own exact success/already-applied logic:
// enable succeeds unless rc!=0 (fail) or the output contains "already
// enabled" (unchanged, not fail — the feature was already on despite
// the list check disagreeing, which the real module treats as
// harmless); disable is considered a real change if rc==0, a no-op if
// rc!=0 but the output matches "Cannot disable feature '<name>'.
// Target file .* does not exist" (i.e. it was already disabled), and a
// failure for any other non-zero exit.
//
// Every command this module runs is prefixed with LANGUAGE=C LC_ALL=C,
// matching real icinga2_feature's own module.run_command_environ_update
// — its own string matching depends on Icinga2's English-locale
// output.
func moduleIcinga2Feature(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("icinga2_feature: state must be present or absent, got %q", state)
	}

	listRes, err := icinga2FeatureExec(ctx, conn, "list")
	if err != nil {
		return Result{}, err
	}
	if listRes.RC != 0 {
		return Fail("Unable to list icinga2 features. Ensure icinga2 is installed and present in binary path."), nil
	}

	disabledRe := regexp.MustCompile(`Disabled features:.* ` + regexp.QuoteMeta(name) + `[ \n]`)
	enabledRe := regexp.MustCompile(`Enabled features:.* ` + regexp.QuoteMeta(name) + `[ \n]`)
	if (disabledRe.MatchString(listRes.Stdout) && state == "absent") ||
		(enabledRe.MatchString(listRes.Stdout) && state == "present") {
		return Ok(""), nil
	}

	verb := "enable"
	if state == "absent" {
		verb = "disable"
	}
	res, err := icinga2FeatureExec(ctx, conn, verb, name)
	if err != nil {
		return Result{}, err
	}

	if state == "present" {
		if res.RC != 0 {
			return Fail("Failed to " + verb + " feature " + name + ". icinga2 command returned " + res.Stdout), nil
		}
		if strings.Contains(res.Stdout, "already enabled") {
			return Ok(""), nil
		}
		return Changed(""), nil
	}

	if res.RC == 0 {
		return Changed(""), nil
	}
	notInstalledRe := regexp.MustCompile(`Cannot disable feature '` + regexp.QuoteMeta(name) + `'\. Target file .* does not exist`)
	if notInstalledRe.MatchString(res.Stdout) {
		return Ok(""), nil
	}
	return Fail("Failed to disable feature. Command returns " + res.Stdout), nil
}

// icinga2FeatureExec runs `LANGUAGE=C LC_ALL=C icinga2 feature <args...>`.
func icinga2FeatureExec(ctx context.Context, conn remoteexec.Connection, args ...string) (remoteexec.Result, error) {
	argv := append([]string{"icinga2", "feature"}, args...)
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return conn.Exec(ctx, "LANGUAGE=C LC_ALL=C "+strings.Join(quoted, " "), nil)
}
