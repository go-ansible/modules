package modules

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSwdepot implements Ansible's `swdepot` (community.general)
// module: installs, upgrades, and removes packages with HP-UX's own
// `swdepot` package manager, via its `swlist`/`swinstall`/`swremove`
// front-ends.
//
// Args: name (string, required, alias pkg); state (string, required —
// one of present, latest, absent); depot (string) — the source
// repository (e.g. "repository:/path") passed to `swinstall -s`;
// required whenever state is present or latest, matching real
// swdepot's own module.
//
// Idempotency: presence and installed version are read via `swlist -a
// revision -l product [-s depot] name`, taking the second
// whitespace-separated field of the line containing name (matching
// real swdepot's own query_package(), which does the same after
// collapsing runs of whitespace). state=latest additionally queries
// the same command scoped to `depot` to read the depot's own version,
// then compares versions the way real swdepot's own compare_package()
// does: strip trailing ".0" groups, split on ".", compare the
// resulting integer lists element-by-element.
//
// Real swdepot's own module carries a few messages this port
// reproduces verbatim since they are what real swdepot actually
// returns, not this port's own wording: state=absent on an
// already-absent package reports msg "No changed" (sic — a typo in
// real swdepot's own default message, left uncorrected here since
// changing it would make this port's output diverge from a real
// swdepot run for no functional reason).
func moduleSwdepot(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		name, err = requireString(args, "pkg")
		if err != nil {
			return Result{}, errArg("swdepot: missing required argument: name (or pkg)")
		}
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	if state != "present" && state != "latest" && state != "absent" {
		return Result{}, errArg("swdepot: state must be present, latest, or absent, got %q", state)
	}
	depot := argString(args, "depot", "")
	if (state == "present" || state == "latest") && depot == "" {
		return Result{}, errArg("swdepot: depot parameter is mandatory in present or latest task")
	}

	installedVer, installed, err := swdepotQuery(ctx, conn, name, "")
	if err != nil {
		return Result{}, err
	}

	switch state {
	case "present":
		if installed {
			return Ok("Already installed").WithExtra("name", name).WithExtra("state", state), nil
		}
		if err := swdepotInstall(ctx, conn, depot, name); err != nil {
			return Result{}, err
		}
		return Changed("Package installed").WithExtra("name", name).WithExtra("state", state), nil

	case "latest":
		if !installed {
			if err := swdepotInstall(ctx, conn, depot, name); err != nil {
				return Result{}, err
			}
			return Changed("Package installed").WithExtra("name", name).WithExtra("state", state), nil
		}
		depotVer, foundInDepot, err := swdepotQuery(ctx, conn, name, depot)
		if err != nil {
			return Result{}, err
		}
		if !foundInDepot {
			return Result{}, fmt.Errorf("swdepot: software package not in repository %s", depot)
		}
		if swdepotCompareVersions(installedVer, depotVer) < 0 {
			if err := swdepotInstall(ctx, conn, depot, name); err != nil {
				return Result{}, err
			}
			return Changed(fmt.Sprintf("Package upgraded, Before %s Now %s", installedVer, depotVer)).
				WithExtra("name", name).WithExtra("state", state), nil
		}
		return Ok("Already installed").WithExtra("name", name).WithExtra("state", state), nil

	default: // absent
		if !installed {
			return Ok("No changed").WithExtra("name", name).WithExtra("state", state), nil
		}
		if err := swdepotRemove(ctx, conn, name); err != nil {
			return Result{}, err
		}
		return Changed("Package removed").WithExtra("name", name).WithExtra("state", state), nil
	}
}

// swdepotQuery runs `swlist -a revision -l product [-s depot] name` and
// returns the version reported on the first output line containing
// name (its second whitespace-separated field), matching real
// swdepot's own query_package(). found is false both when the command
// exits non-zero and when no matching line is found in its output.
func swdepotQuery(ctx context.Context, conn remoteexec.Connection, name, depot string) (version string, found bool, err error) {
	tokens := []string{"swlist", "-a", "revision", "-l", "product"}
	if depot != "" {
		tokens = append(tokens, "-s", depot)
	}
	tokens = append(tokens, name)
	res, err := runStatus(ctx, conn, quoteAll(tokens))
	if err != nil {
		return "", false, err
	}
	if res.RC != 0 {
		return "", false, nil
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if !strings.Contains(line, name) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			return fields[1], true, nil
		}
	}
	return "", false, nil
}

func swdepotInstall(ctx context.Context, conn remoteexec.Connection, depot, name string) error {
	tokens := []string{"swinstall", "-x", "mount_all_filesystems=false", "-s", depot, name}
	_, err := run(ctx, conn, quoteAll(tokens))
	return err
}

func swdepotRemove(ctx context.Context, conn remoteexec.Connection, name string) error {
	_, err := run(ctx, conn, "swremove "+shellQuote(name))
	return err
}

var swdepotTrailingZerosRE = regexp.MustCompile(`(\.0+)*$`)

// swdepotNormalizeVersion strips trailing ".0" groups and splits the
// remainder on "." into integers, matching real swdepot's own
// compare_package() normalize().
func swdepotNormalizeVersion(v string) []int {
	v = swdepotTrailingZerosRE.ReplaceAllString(v, "")
	if v == "" {
		return []int{0}
	}
	parts := strings.Split(v, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, _ := strconv.Atoi(p)
		out[i] = n
	}
	return out
}

// swdepotCompareVersions returns -1/0/1 the way real swdepot's own
// compare_package() does: normalize both versions, then compare the
// resulting integer lists element-by-element (a shorter list that
// matches its prefix compares as less-than, matching Python's own list
// comparison, which is what real swdepot's own module relies on).
func swdepotCompareVersions(a, b string) int {
	na, nb := swdepotNormalizeVersion(a), swdepotNormalizeVersion(b)
	for i := 0; i < len(na) && i < len(nb); i++ {
		if na[i] < nb[i] {
			return -1
		}
		if na[i] > nb[i] {
			return 1
		}
	}
	switch {
	case len(na) < len(nb):
		return -1
	case len(na) > len(nb):
		return 1
	default:
		return 0
	}
}
