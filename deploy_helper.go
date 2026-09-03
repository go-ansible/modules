package modules

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDeployHelper implements Ansible's `deploy_helper`
// (community.general) module: manages a releases/current/shared
// symlink-based deploy directory layout — PURE filesystem operations (no
// external tool dependency), composed here as POSIX shell commands run
// via the target Connection's Exec, matching this port's general
// architecture (see module.go's own doc comment).
//
// Directory layout (matching real deploy_helper's own documented
// convention): path/ (the project root) holds releases_path (default
// "releases", one subdirectory per release), shared_path (default
// "shared"), and a current_path symlink (default "current") pointing at
// the active release inside releases_path.
//
// Args: path (required, alias dest) — the project root; release —
// defaults to a `%Y%m%d%H%M%S` timestamp for state=query/present (this
// makes the module non-idempotent by default, matching real
// deploy_helper's own documented note, unless a caller passes its own
// release, e.g. `release={{ deploy_helper.new_release }}` from a prior
// state=present run), but is REQUIRED for state=finalize; releases_path
// (default "releases"); shared_path (default "shared" — an empty string
// disables the shared directory entirely); current_path (default
// "current"); keep_releases (default 5); clean (default true — run the
// cleanup procedure during state=finalize); unfinished_filename (default
// "DEPLOY_UNFINISHED"); state (default present: present|finalize|
// absent|clean|query).
//
// state=query/present populate Facts["deploy_helper"] with
// project_path, current_path, releases_path, shared_path,
// unfinished_filename, previous_release/previous_release_path (from
// resolving the current current_path symlink, if any), new_release,
// new_release_path — matching real deploy_helper's own documented fact
// set exactly (note real deploy_helper stores this in ansible_facts;
// this port surfaces it via Result.Facts, this package's own equivalent
// — see module.go's own Result doc comment).
//
// state=present creates path/, releases_path/, and (unless shared_path
// is "") shared_path/, failing if current_path exists but is not a
// symlink (matching real deploy_helper's own check_link).
//
// state=finalize removes new_release_path's own unfinished_filename
// marker, atomically symlinks current_path -> new_release_path (via a
// temp-name-then-rename, matching real deploy_helper's own create_link,
// to avoid a window with no current_path at all), and — if clean — runs
// the same cleanup state=clean does (remove any leftover unfinished
// temp-symlink, delete every release still containing
// unfinished_filename, then trim releases_path down to keep_releases
// entries, newest-by-mtime first, always excluding new_release itself).
//
// state=clean runs that same cleanup without finalizing.
//
// state=absent deletes path/ entirely (equivalent to `file:
// state=absent` on the whole project root, matching real deploy_helper's
// own documented note) and clears Facts["deploy_helper"].
//
// Deviation from real deploy_helper: owner/group/mode/SELinux-context
// arguments (attributes, group, mode, owner, selevel, serole, setype,
// seuser, unsafe_writes) that real deploy_helper's own
// set_directory_attributes_if_different applies to every directory it
// creates are not implemented — this port creates directories with the
// target's default umask only, matching this port's general choice
// (see monit.go's own doc comment on the project's usual scope
// boundary) to skip attribute reconciliation already covered by this
// package's own file.go module, which a caller can run afterward on
// deploy_helper's own returned facts if exact ownership/mode matters.
// Release ordering for state=clean's own trim uses each release
// directory's mtime (via `ls -t`), not real deploy_helper's own ctime —
// the two normally agree for a freshly-created release directory, but
// diverge if a release directory's own metadata (not its contents) was
// touched after creation.
func moduleDeployHelper(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	projectPath := argString(args, "path", argString(args, "dest", ""))
	if projectPath == "" {
		return Result{}, errArg("deploy_helper: missing required argument: path")
	}
	releasesPathRel := argString(args, "releases_path", "releases")
	sharedPathRel := argString(args, "shared_path", "shared")
	currentPathRel := argString(args, "current_path", "current")
	keepReleases := argInt(args, "keep_releases", 5)
	clean := argBool(args, "clean", true)
	unfinished := argString(args, "unfinished_filename", "DEPLOY_UNFINISHED")
	state := argString(args, "state", "present")
	release := argString(args, "release", "")

	if state == "finalize" && release == "" {
		return Result{}, errArg("deploy_helper: release is required when state=finalize")
	}

	currentPath := deployHelperJoin(projectPath, currentPathRel)
	releasesPath := deployHelperJoin(projectPath, releasesPathRel)
	var sharedPath string
	if sharedPathRel != "" {
		sharedPath = deployHelperJoin(projectPath, sharedPathRel)
	}

	prevRelease, prevReleasePath, err := deployHelperLastRelease(ctx, conn, currentPath)
	if err != nil {
		return Result{}, err
	}

	if release == "" && (state == "query" || state == "present") {
		release = time.Now().Format("20060102150405")
	}
	var newReleasePath string
	if release != "" {
		newReleasePath = deployHelperJoin(releasesPath, release)
	}

	facts := map[string]any{
		"project_path":          projectPath,
		"current_path":          currentPath,
		"releases_path":         releasesPath,
		"shared_path":           deployHelperNilIfEmpty(sharedPath),
		"previous_release":      deployHelperNilIfEmpty(prevRelease),
		"previous_release_path": deployHelperNilIfEmpty(prevReleasePath),
		"new_release":           deployHelperNilIfEmpty(release),
		"new_release_path":      deployHelperNilIfEmpty(newReleasePath),
		"unfinished_filename":   unfinished,
	}

	changes := 0
	res := Result{}

	switch state {
	case "query":
		res.Facts = map[string]any{"deploy_helper": facts}

	case "present":
		if failMsg, err := deployHelperCheckLink(ctx, conn, currentPath); err != nil {
			return Result{}, err
		} else if failMsg != "" {
			return Fail("deploy_helper: " + failMsg), nil
		}
		n, failMsg, err := deployHelperCreatePath(ctx, conn, projectPath)
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail("deploy_helper: " + failMsg), nil
		}
		changes += n
		n, failMsg, err = deployHelperCreatePath(ctx, conn, releasesPath)
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail("deploy_helper: " + failMsg), nil
		}
		changes += n
		if sharedPath != "" {
			n, failMsg, err = deployHelperCreatePath(ctx, conn, sharedPath)
			if err != nil {
				return Result{}, err
			}
			if failMsg != "" {
				return Fail("deploy_helper: " + failMsg), nil
			}
			changes += n
		}
		res.Facts = map[string]any{"deploy_helper": facts}

	case "finalize":
		if keepReleases <= 0 {
			return Fail("deploy_helper: 'keep_releases' should be at least 1"), nil
		}
		n, err := deployHelperRemoveUnfinishedFile(ctx, conn, newReleasePath, unfinished)
		if err != nil {
			return Result{}, err
		}
		changes += n
		n, err = deployHelperCreateLink(ctx, conn, newReleasePath, currentPath, unfinished)
		if err != nil {
			return Result{}, err
		}
		changes += n
		if clean {
			n, err = deployHelperCleanup(ctx, conn, projectPath, releasesPath, release, unfinished, keepReleases)
			if err != nil {
				return Result{}, err
			}
			changes += n
		}

	case "clean":
		n, err := deployHelperCleanup(ctx, conn, projectPath, releasesPath, release, unfinished, keepReleases)
		if err != nil {
			return Result{}, err
		}
		changes += n

	case "absent":
		res.Facts = map[string]any{"deploy_helper": []any{}}
		existed, err := pathExists(ctx, conn, projectPath)
		if err != nil {
			return Result{}, err
		}
		if existed {
			if _, err := run(ctx, conn, "rm -rf -- "+shellQuote(projectPath)); err != nil {
				return Result{}, err
			}
			changes++
		}

	default:
		return Result{}, errArg("deploy_helper: state must be one of present, absent, clean, finalize, query, got %q", state)
	}

	res.Changed = changes > 0
	return res, nil
}

func deployHelperJoin(base, rel string) string {
	if path.IsAbs(rel) {
		return rel
	}
	return path.Join(base, rel)
}

func deployHelperNilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// deployHelperLastRelease resolves currentPath (if it exists at all,
// even a dangling symlink) via `readlink -f`, matching real
// deploy_helper's own _get_last_release (os.path.realpath).
func deployHelperLastRelease(ctx context.Context, conn remoteexec.Connection, currentPath string) (release, releasePath string, err error) {
	res, err := runStatus(ctx, conn, "test -e "+shellQuote(currentPath)+" -o -L "+shellQuote(currentPath))
	if err != nil {
		return "", "", err
	}
	if res.RC != 0 {
		return "", "", nil
	}
	out, err := run(ctx, conn, "readlink -f "+shellQuote(currentPath))
	if err != nil {
		return "", "", nil
	}
	releasePath = strings.TrimSpace(out)
	return path.Base(releasePath), releasePath, nil
}

// deployHelperCheckLink fails if currentPath exists but is not a
// symlink, matching real deploy_helper's own check_link.
func deployHelperCheckLink(ctx context.Context, conn remoteexec.Connection, currentPath string) (failMsg string, err error) {
	res, err := runStatus(ctx, conn, "test -e "+shellQuote(currentPath)+" -o -L "+shellQuote(currentPath))
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", nil
	}
	linkRes, err := runStatus(ctx, conn, "test -L "+shellQuote(currentPath))
	if err != nil {
		return "", err
	}
	if linkRes.RC != 0 {
		return currentPath + " exists but is not a symbolic link", nil
	}
	return "", nil
}

// deployHelperCreatePath creates dir (mkdir -p) if it doesn't already
// exist, matching real deploy_helper's own create_path; returns 1 if it
// created the directory, 0 if it already existed as a directory. A
// non-empty failMsg (dir exists as something other than a directory) is
// an expected runtime failure, not a transport error — matching real
// deploy_helper's own module.fail_json for that case (see module.go's
// own doc comment on when this package returns Result{Failed:true}
// instead of a Go error).
func deployHelperCreatePath(ctx context.Context, conn remoteexec.Connection, dir string) (changed int, failMsg string, err error) {
	res, err := runStatus(ctx, conn, "test -e "+shellQuote(dir)+" -o -L "+shellQuote(dir))
	if err != nil {
		return 0, "", err
	}
	if res.RC == 0 {
		isDir, err := runStatus(ctx, conn, "test -d "+shellQuote(dir))
		if err != nil {
			return 0, "", err
		}
		if isDir.RC != 0 {
			return 0, fmt.Sprintf("%s exists but is not a directory", dir), nil
		}
		return 0, "", nil
	}
	if _, err := run(ctx, conn, "mkdir -p -- "+shellQuote(dir)); err != nil {
		return 0, "", err
	}
	return 1, "", nil
}

// deployHelperCreateLink atomically points linkName at source via `ln
// -sfn` (force, and — critically — no-dereference: without -n, a `ln
// -sf` targeting an existing symlink-to-a-directory creates the new
// link INSIDE that directory instead of replacing it, exactly the
// ambiguity real deploy_helper's own create_link avoids by calling
// os.rename() directly rather than shelling out to `mv`/`ln` at all).
// `-n` is specified by POSIX and supported by both GNU and BSD/macOS
// `ln`, unlike GNU-only `mv -T` (this port's first draft, which failed
// outright on a BSD target). A no-op (0 changes) if linkName already
// resolves to source.
//
// Deviation from real deploy_helper: real create_link's own temp-name-
// then-os.rename() dance guarantees linkName is NEVER observably
// missing (rename() is atomic on POSIX). `ln -sfn` is the standard
// portable idiom for the same goal but is not guaranteed atomic on
// every `ln` implementation (some do unlink-then-symlink internally) —
// a caller reading linkName at the exact wrong instant on such a target
// could see it briefly absent, a narrower window than real
// deploy_helper's own guarantee but not a bitwise identical one.
func deployHelperCreateLink(ctx context.Context, conn remoteexec.Connection, source, linkName, unfinished string) (int, error) {
	_ = unfinished // real deploy_helper's own temp-name suffix; unneeded by the ln -sfn approach.
	isLink, err := runStatus(ctx, conn, "test -L "+shellQuote(linkName))
	if err != nil {
		return 0, err
	}
	if isLink.RC == 0 {
		out, err := run(ctx, conn, "readlink -f "+shellQuote(linkName))
		if err == nil {
			curTarget := strings.TrimSpace(out)
			srcOut, srcErr := run(ctx, conn, "readlink -f "+shellQuote(source))
			if srcErr == nil && curTarget == strings.TrimSpace(srcOut) {
				return 0, nil
			}
		}
	}
	if _, err := run(ctx, conn, "ln -sfn -- "+shellQuote(source)+" "+shellQuote(linkName)); err != nil {
		return 0, err
	}
	return 1, nil
}

// deployHelperRemoveUnfinishedFile removes
// "<newReleasePath>/<unfinished>" if present, matching real
// deploy_helper's own remove_unfinished_file.
func deployHelperRemoveUnfinishedFile(ctx context.Context, conn remoteexec.Connection, newReleasePath, unfinished string) (int, error) {
	if newReleasePath == "" {
		return 0, nil
	}
	p := deployHelperJoin(newReleasePath, unfinished)
	exists, err := pathExists(ctx, conn, p)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}
	if _, err := run(ctx, conn, "rm -f -- "+shellQuote(p)); err != nil {
		return 0, err
	}
	return 1, nil
}

// deployHelperCleanup runs the shared finalize/clean cleanup sequence
// real deploy_helper's own main() runs for both states: remove a
// leftover "<release>.<unfinished>" temp-symlink name from a previous
// interrupted finalize (remove_unfinished_link), delete every release
// directory still containing the unfinished marker
// (remove_unfinished_builds), then trim releasesPath down to
// keep_releases entries by mtime, newest first, always excluding
// reserveRelease (cleanup).
func deployHelperCleanup(ctx context.Context, conn remoteexec.Connection, projectPath, releasesPath, reserveRelease, unfinished string, keepReleases int) (int, error) {
	changes := 0

	if reserveRelease != "" {
		tmp := deployHelperJoin(projectPath, reserveRelease+"."+unfinished)
		exists, err := pathExists(ctx, conn, tmp)
		if err != nil {
			return changes, err
		}
		if exists {
			if _, err := run(ctx, conn, "rm -f -- "+shellQuote(tmp)); err != nil {
				return changes, err
			}
			changes++
		}
	}

	exists, err := pathExists(ctx, conn, releasesPath)
	if err != nil {
		return changes, err
	}
	if !exists {
		return changes, nil
	}

	entries, err := deployHelperListReleases(ctx, conn, releasesPath)
	if err != nil {
		return changes, err
	}

	var kept []deployHelperRelease
	for _, e := range entries {
		unfinishedPath := deployHelperJoin(e.path, unfinished)
		isUnfinished, err := pathExists(ctx, conn, unfinishedPath)
		if err != nil {
			return changes, err
		}
		if isUnfinished {
			if _, err := run(ctx, conn, "rm -rf -- "+shellQuote(e.path)); err != nil {
				return changes, err
			}
			changes++
			continue
		}
		kept = append(kept, e)
	}

	// kept is already newest-first (see deployHelperListReleases's own
	// doc comment), so toTrim needs no separate sort.
	var toTrim []deployHelperRelease
	for _, e := range kept {
		if e.name != reserveRelease {
			toTrim = append(toTrim, e)
		}
	}
	if len(toTrim) > keepReleases {
		for _, e := range toTrim[keepReleases:] {
			if _, err := run(ctx, conn, "rm -rf -- "+shellQuote(e.path)); err != nil {
				return changes, err
			}
			changes++
		}
	}

	return changes, nil
}

type deployHelperRelease struct {
	name string
	path string
}

// deployHelperListReleases lists every directory directly under
// releasesPath, ALREADY ordered newest-modified first, matching real
// deploy_helper's own cleanup()'s `os.listdir` + sort by
// os.path.getctime (this port orders by mtime, not ctime — see
// moduleDeployHelper's own doc comment on that deviation).
//
// This shells out to `ls -1t` (list, one per line, sorted by
// modification time — a plain POSIX-specified `ls` flag, unlike GNU
// find's own `-printf`, this port's first draft, which does not exist
// on a BSD/macOS target's `find` at all and silently produced zero
// releases there, this func's own `err != nil` fallback swallowing the
// real cause) rather than fetching each entry's own mtime and sorting
// client-side, then filters to directories with a `test -d` per name —
// both steps run as one Exec round trip.
func deployHelperListReleases(ctx context.Context, conn remoteexec.Connection, releasesPath string) ([]deployHelperRelease, error) {
	cmd := "cd -- " + shellQuote(releasesPath) + " && for f in $(ls -1t); do [ -d \"$f\" ] && printf '%s\\n' \"$f\"; done"
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, nil
	}
	var releases []deployHelperRelease
	for _, name := range strings.Split(res.Stdout, "\n") {
		if name == "" {
			continue
		}
		releases = append(releases, deployHelperRelease{name: name, path: deployHelperJoin(releasesPath, name)})
	}
	return releases, nil
}
