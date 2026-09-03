package modules

import (
	"context"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// uvPythonEntry is one line of `uv python list`'s own output.
type uvPythonEntry struct {
	Version   string // e.g. "3.13.5"
	Path      string // installed python binary path, empty if not installed
	Installed bool
}

// uvPythonVersionRE extracts the numeric version from a `uv python
// list` key like "cpython-3.13.5-macos-aarch64-none" or
// "cpython-3.13.5rc1-linux-x86_64-gnu". Only CPython entries are
// matched — see moduleUvPython's own doc comment for why this port
// narrows real uv_python's own broader implementation support
// (PyPy/GraalPy) to CPython only.
var uvPythonVersionRE = regexp.MustCompile(`^cpython-(\d+\.\d+(?:\.\d+)?[a-zA-Z0-9]*)-`)

// uvPythonList runs `uv python list --all-versions` (available AND
// installed, matching real uv_python's own need to resolve a bare
// minor version like "3.12" against the newest AVAILABLE patch, not
// just the newest already-installed one) and parses it.
func uvPythonList(ctx context.Context, conn remoteexec.Connection) ([]uvPythonEntry, error) {
	res, err := runStatus(ctx, conn, "uv python list --all-versions 2>/dev/null")
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, nil
	}
	var out []uvPythonEntry
	for _, line := range strings.Split(res.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		m := uvPythonVersionRE.FindStringSubmatch(fields[0])
		if m == nil {
			continue
		}
		entry := uvPythonEntry{Version: m[1]}
		if len(fields) >= 2 && !strings.HasPrefix(fields[1], "<") {
			entry.Installed = true
			entry.Path = fields[1]
		}
		out = append(out, entry)
	}
	return out, nil
}

// uvPythonResolve finds the entry uv itself would pick for a version
// selector: an exact match if version has a patch component (three
// dot-separated numbers, or a pre-release suffix), else the first
// (newest — `uv python list` prints newest-first) entry whose version
// starts with "<version>.".
func uvPythonResolve(entries []uvPythonEntry, version string) (uvPythonEntry, bool) {
	isExact := strings.Count(version, ".") >= 2
	for _, e := range entries {
		if isExact {
			if e.Version == version {
				return e, true
			}
			continue
		}
		if e.Version == version || strings.HasPrefix(e.Version, version+".") {
			return e, true
		}
	}
	return uvPythonEntry{}, false
}

// moduleUvPython implements Ansible's `uv_python` (community.general)
// module: installs, uninstalls, or upgrades a Python version managed
// by Astral's `uv` tool, via the `uv python` CLI subcommand — real
// uv_python already shells out to `uv` itself (there is no separate
// library to substitute; `uv` IS the tool being managed), so this
// port's architecture matches real uv_python's own here.
//
// Args: version (required) — "3.12", "3.12.3", or a pre-release like
// "3.15.0a5"; advanced uv selectors ("cpython@3.12", ">=3.12,<3.13")
// are rejected up front, matching real uv_python's own documented
// "not supported in this release" note; state (present|absent|latest,
// default "present").
//
// This port only recognizes CPython entries from `uv python list`'s
// own output (lines starting "cpython-") — real uv_python (and `uv`
// itself) also manages PyPy and GraalPy interpreters, but this
// module's own `version` argument shape ("3.12", "3.12.3") has no way
// to select those without an implementation qualifier in the first
// place, so narrowing to CPython costs nothing real uv_python's own
// documented `version` argument shape could reach anyway.
//
// state=present: resolves `version` against `uv python list
// --all-versions`'s own newest-first ordering (uvPythonResolve) — an
// exact patch version installs only that patch; a bare minor version
// installs its own newest available patch ONLY IF NO patch of that
// minor is already installed (matching real uv_python's own
// documented "only if no patch version for that minor release is
// currently installed" note) via `uv python install <resolved
// version>`. state=absent: an exact patch version uninstalls only
// that patch; a bare minor version uninstalls every currently
// installed patch of that minor (`uv python uninstall <patch>` per
// match, matching real uv_python's own documented "all installed
// patch versions... are removed" note). state=latest: resolves the
// same way present's minor-version path does, but always (not just
// when nothing of that minor is installed yet) ensures the resolved
// newest patch is installed — this port does this by checking whether
// the resolved patch itself is already installed and, if not, running
// `uv python install <resolved version>`, which is the same
// observable end state real uv_python's own explicitly-NOT-using-
// `uv python upgrade` implementation reaches (see real uv_python's own
// documented note that this state doesn't call `uv python upgrade`);
// state=latest does not uninstall a coexisting older patch of the
// same minor, matching real uv_python's own documented behavior.
//
// Extra["python_versions"]/Extra["python_paths"]: the versions/paths
// this run actually changed (installed or removed), matching real
// uv_python's own documented return shape; Extra["rc"]/["stdout"]/
// ["stderr"] from the last `uv python install`/`uninstall` invocation
// this port issued (empty/0 if none were needed).
func moduleUvPython(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	version, err := requireString(args, "version")
	if err != nil {
		return Result{}, err
	}
	if strings.ContainsAny(version, "<>=@,") {
		return Result{}, errArg("uv_python: version %q uses an advanced uv selector, which this port (and real uv_python) does not support", version)
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" && state != "latest" {
		return Result{}, errArg("uv_python: state must be one of present, absent, latest, got %q", state)
	}

	entries, err := uvPythonList(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	switch state {
	case "present":
		return uvPythonEnsurePresent(ctx, conn, entries, version, false)
	case "latest":
		return uvPythonEnsurePresent(ctx, conn, entries, version, true)
	default: // absent
		return uvPythonEnsureAbsent(ctx, conn, entries, version)
	}
}

func uvPythonEnsurePresent(ctx context.Context, conn remoteexec.Connection, entries []uvPythonEntry, version string, latest bool) (Result, error) {
	resolved, ok := uvPythonResolve(entries, version)
	if !ok {
		resolved = uvPythonEntry{Version: version}
	}

	if !latest {
		// present: unchanged if ANY installed patch of the requested
		// minor (or the exact patch) already exists.
		for _, e := range entries {
			if !e.Installed {
				continue
			}
			if e.Version == version || strings.HasPrefix(e.Version, version+".") {
				return Ok(e.Version+" already installed").
					WithExtra("python_versions", []string{}).
					WithExtra("python_paths", []string{}), nil
			}
		}
	} else if resolved.Installed {
		return Ok(resolved.Version+" already the latest installed").
			WithExtra("python_versions", []string{}).
			WithExtra("python_paths", []string{}), nil
	}

	target := resolved.Version
	res, err := conn.Exec(ctx, "uv python install "+shellQuote(target), nil)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("uv_python: installing "+target+": "+strings.TrimSpace(res.Stderr)).
			WithExtra("rc", res.RC).WithExtra("stdout", res.Stdout).WithExtra("stderr", res.Stderr), nil
	}
	entries, err = uvPythonList(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	installed, _ := uvPythonResolve(entries, target)
	result := Changed(target+" installed").
		WithExtra("python_versions", []string{installed.Version}).
		WithExtra("rc", res.RC).WithExtra("stdout", res.Stdout).WithExtra("stderr", res.Stderr)
	if installed.Path != "" {
		result = result.WithExtra("python_paths", []string{installed.Path})
	} else {
		result = result.WithExtra("python_paths", []string{})
	}
	return result, nil
}

func uvPythonEnsureAbsent(ctx context.Context, conn remoteexec.Connection, entries []uvPythonEntry, version string) (Result, error) {
	isExact := strings.Count(version, ".") >= 2
	var toRemove []uvPythonEntry
	for _, e := range entries {
		if !e.Installed {
			continue
		}
		if isExact {
			if e.Version == version {
				toRemove = append(toRemove, e)
			}
			continue
		}
		if e.Version == version || strings.HasPrefix(e.Version, version+".") {
			toRemove = append(toRemove, e)
		}
	}
	if len(toRemove) == 0 {
		return Ok(version+" already absent").
			WithExtra("python_versions", []string{}).
			WithExtra("python_paths", []string{}), nil
	}

	var versions, paths []string
	var lastRes remoteexec.Result
	for _, e := range toRemove {
		res, err := conn.Exec(ctx, "uv python uninstall "+shellQuote(e.Version), nil)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("uv_python: uninstalling "+e.Version+": "+strings.TrimSpace(res.Stderr)).
				WithExtra("rc", res.RC).WithExtra("stdout", res.Stdout).WithExtra("stderr", res.Stderr), nil
		}
		versions = append(versions, e.Version)
		paths = append(paths, e.Path)
		lastRes = res
	}
	return Changed(strings.Join(versions, ", ")+" removed").
		WithExtra("python_versions", versions).
		WithExtra("python_paths", paths).
		WithExtra("rc", lastRes.RC).WithExtra("stdout", lastRes.Stdout).WithExtra("stderr", lastRes.Stderr), nil
}
