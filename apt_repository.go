package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAptRepository implements (a subset of) Ansible's
// `apt_repository` module: adds or removes an APT source, either a
// literal `deb ...` line (written to a file derived from the line,
// under /etc/apt/sources.list.d/) or a `ppa:user/name` shorthand
// (delegated to `add-apt-repository`, matching what real Ansible itself
// does under the hood for that form rather than reimplementing PPA URL
// construction).
//
// Args: repo (string, required) — a `deb ...` source line, or a
// `ppa:user/name` shorthand; state (present|absent, default "present");
// update_cache (bool, default false — real ansible.builtin.apt_repository
// defaults this to true; this port defaults to false per this batch's
// task spec, a deliberate deviation documented here).
//
// Simplifications vs real apt_repository: no `filename` (the
// destination filename is always derived from repo, not user-settable),
// `mode`, `codename`, `validate_certs`, or cache-retry knobs. For the
// `ppa:` form, idempotency is not checked before invoking
// add-apt-repository — like apt.go's "latest" state, a no-op
// add-apt-repository still exits 0 and this port can't cheaply tell
// "already added" apart without parsing its output, so that form is
// always reported changed. For the plain `deb ...` line form,
// idempotency IS checked, by comparing the derived file's existing
// content against the wanted line.
func moduleAptRepository(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	repo, err := requireString(args, "repo")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("apt_repository: state must be present or absent, got %q", state)
	}
	updateCache := argBool(args, "update_cache", false)

	var changed bool
	if strings.HasPrefix(repo, "ppa:") {
		changed, err = aptRepositoryPPA(ctx, conn, repo, state)
	} else {
		changed, err = aptRepositoryLine(ctx, conn, repo, state)
	}
	if err != nil {
		return Result{}, err
	}

	if changed && updateCache {
		if _, err := run(ctx, conn, "DEBIAN_FRONTEND=noninteractive apt-get update -q"); err != nil {
			return Result{}, err
		}
	}

	if changed {
		return Changed(repo), nil
	}
	return Ok(repo + " unchanged"), nil
}

// aptRepositoryPPA adds or removes repo (a "ppa:..." shorthand) via
// add-apt-repository, always reporting changed=true (see the doc
// comment on moduleAptRepository).
func aptRepositoryPPA(ctx context.Context, conn remoteexec.Connection, repo, state string) (bool, error) {
	cmd := "add-apt-repository -y " + shellQuote(repo)
	if state == "absent" {
		cmd = "add-apt-repository -y --remove " + shellQuote(repo)
	}
	if _, err := run(ctx, conn, cmd); err != nil {
		return false, err
	}
	return true, nil
}

// aptRepositoryLine adds or removes repo (a literal "deb ..." line) by
// writing/removing a file derived from it under
// /etc/apt/sources.list.d/, idempotently.
func aptRepositoryLine(ctx context.Context, conn remoteexec.Connection, repo, state string) (bool, error) {
	path := aptRepoFilename(repo)
	exists, err := pathExists(ctx, conn, path)
	if err != nil {
		return false, err
	}

	if state == "absent" {
		if !exists {
			return false, nil
		}
		if _, err := run(ctx, conn, "rm -f "+shellQuote(path)); err != nil {
			return false, err
		}
		return true, nil
	}

	// state == "present"
	if exists {
		current, err := run(ctx, conn, "cat "+shellQuote(path))
		if err != nil {
			return false, err
		}
		if current == repo {
			return false, nil
		}
	}
	cmd := "mkdir -p /etc/apt/sources.list.d && printf '%s\\n' " + shellQuote(repo) + " > " + shellQuote(path)
	if _, err := run(ctx, conn, cmd); err != nil {
		return false, err
	}
	return true, nil
}

// aptRepoFilename derives a sources.list.d filename from a repo string
// by replacing every non-alphanumeric character with '-' and collapsing
// runs of '-', matching the shape (not the exact algorithm) of real
// apt_repository's own auto-derived filenames.
func aptRepoFilename(repo string) string {
	var b strings.Builder
	for _, r := range repo {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	if name == "" {
		name = "repository"
	}
	if len(name) > 60 {
		name = name[:60]
	}
	return "/etc/apt/sources.list.d/" + name + ".list"
}
