package modules

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSorcery implements Ansible's `sorcery` (community.general)
// module: manages Source Mage GNU/Linux's own "spells" (packages) and
// "grimoires" (spell collections/repositories) via the Sorcery
// toolchain (`sorcery`/`scribe`/`cast`/`dispel`/`gaze`).
//
// Args: name (string or []string, aliases spell/grimoire; a single
// comma-separated string is split into multiple names, matching real
// sorcery's own `type: list` argument, which Ansible auto-splits on
// commas) — spell name(s), or (with `repository` set) grimoire
// name(s); the special value "*" with state=latest means "update the
// whole system" and with state=rebuild means "rebuild the whole
// system"; repository (string, optional) — when set, `name` names
// grimoire(s) instead of spells: the special value "*" pulls/removes
// them from Sorcery's official grimoire list via `scribe add`/`scribe
// remove`, while any other value is a single-grimoire source URL for
// `scribe add <name> from <url>` (state=absent is only valid with
// repository="*", matching real sorcery's own documented restriction,
// and only a single name is allowed with a non-"*" repository); state
// (string, default "present" — present/cast are aliases of each
// other, as are absent/dispelled; latest; rebuild); depends (string,
// optional) — comma-separated optional-dependency toggles for a
// single spell (`dep`, `+dep` = on, `-dep` = off; a `name(PROVIDER)`
// form is passed through verbatim, matching real sorcery's own
// provider syntax); update (bool, default false) — run `sorcery
// update` first; update_cache (bool, default false, alias
// update_codex) — run `scribe update` (scoped to `name` when
// `repository` is set) before acting on spells; cache_valid_time (int
// seconds, default 0) — skip `scribe update` when every relevant
// grimoire's `/var/state/sorcery/<grimoire>.lastupdate` mtime is newer
// than this many seconds ago.
//
// At least one of name/update/update_cache is required, matching real
// sorcery's own required_one_of; when several apply, the sequence is
// always Sorcery -> Grimoire(s) -> Codex -> Spell(s), same as real
// sorcery's own NOTES describe and not overridable.
//
// Idempotency: spell state is read via `gaze -q version <names>`,
// whose pipe-delimited output ("...|...|spell|grimoire_version|
// installed_version", installed_version=="-" meaning not installed)
// drives present (cast only spells reporting "-"), latest (cast only
// spells whose grimoire and installed versions differ), rebuild
// (always cast every named spell), and absent (dispel only spells
// reporting an installed version). Grimoire presence is read via
// `scribe index`, whose "[N] : grimoire : path[ : version]" rows this
// port matches with a regexp rather than real sorcery's own fixed
// header/footer line-count slicing.
//
// Simplifications vs real sorcery:
//   - `update`'s changed status is reported true whenever `sorcery
//     update` exits 0; real sorcery's own module instead diffs
//     `sorcery --version` before/after and only reports changed on an
//     actual version bump.
//   - `depends` is applied by appending
//     `spell:dep:on|off:optional::` lines directly to
//     `/var/state/sorcery/depends` and then unconditionally casting
//     that spell. Real sorcery's own module first validates each
//     dependency with `gaze -q version`, then does a read-modify pass
//     over the depends file to update an existing matching line in
//     place (avoiding duplicates) and only casts if a dependency
//     actually needed to change. This port does neither the
//     validation nor the dedup pass, so repeated runs with the same
//     `depends` keep appending lines and keep reporting changed.
//   - No check_mode support — this port's architecture has none at
//     all, for any module.
func moduleSorcery(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names := sorceryNameList(args)
	repo := argString(args, "repository", "")
	rawState := argString(args, "state", "present")
	state := rawState
	switch state {
	case "cast":
		state = "present"
	case "dispelled":
		state = "absent"
	}
	if state != "present" && state != "latest" && state != "absent" && state != "rebuild" {
		return Result{}, errArg("sorcery: state must be one of present, latest, absent, cast, dispelled, rebuild, got %q", rawState)
	}
	depends := argString(args, "depends", "")
	update := argBool(args, "update", false)
	updateCache := argBool(args, "update_cache", argBool(args, "update_codex", false))
	cacheValidTime := argInt(args, "cache_valid_time", 0)

	if len(names) == 0 && !update && !updateCache {
		return Result{}, errArg("sorcery: one of name, update, or update_cache is required")
	}

	changed := false
	var msgs []string

	if update {
		if _, err := run(ctx, conn, "sorcery update"); err != nil {
			return Result{}, err
		}
		changed = true
		msgs = append(msgs, "successfully updated Sorcery")
	}

	if len(names) > 0 && repo != "" {
		c, msg, err := sorceryManageGrimoires(ctx, conn, names, repo, state)
		if err != nil {
			return Result{}, err
		}
		changed = changed || c
		msgs = append(msgs, msg)
	}

	if updateCache {
		c, msg, err := sorceryUpdateCodex(ctx, conn, names, repo, cacheValidTime)
		if err != nil {
			return Result{}, err
		}
		changed = changed || c
		msgs = append(msgs, msg)
	}

	if len(names) > 0 && repo == "" {
		c, msg, err := sorceryManageSpells(ctx, conn, names, state, depends)
		if err != nil {
			return Result{}, err
		}
		changed = changed || c
		msgs = append(msgs, msg)
	}

	msg := strings.Join(msgs, "; ")
	if changed {
		return Changed(msg), nil
	}
	return Ok(msg), nil
}

// sorceryNameList resolves the `name` argument (or its `spell`/
// `grimoire` aliases), splitting a single comma-separated string into
// multiple elements — matching real sorcery's own `type: list`
// argument, which Ansible auto-splits plain comma-separated strings on
// (real sorcery's own EXAMPLES rely on exactly this: `spell:
// foo,bar,baz`).
func sorceryNameList(args map[string]any) []string {
	raw := argStringList(args, "name")
	if raw == nil {
		raw = argStringList(args, "spell")
	}
	if raw == nil {
		raw = argStringList(args, "grimoire")
	}
	if len(raw) != 1 {
		return raw
	}
	parts := strings.Split(raw[0], ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

var sorceryCodexLineRE = regexp.MustCompile(`\[\d+\]\s*:\s*([\w\-+.]+)\s*:\s*[\w\-+./]+(?:\s*:\s*([\w\-+.]+))?`)

// sorceryCodexIndex parses `scribe index` into grimoire name -> version
// ("N/A" when no version is reported), matching real sorcery's own
// codex_list() parsing of "[N] : grimoire : path[ : version]" rows.
func sorceryCodexIndex(ctx context.Context, conn remoteexec.Connection) (map[string]string, error) {
	out, err := run(ctx, conn, "scribe index")
	if err != nil {
		return nil, fmt.Errorf("sorcery: unable to list grimoire collection, fix your Codex: %w", err)
	}
	codex := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		m := sorceryCodexLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ver := m[2]
		if ver == "" {
			ver = "N/A"
		}
		codex[m[1]] = ver
	}
	return codex, nil
}

func sorceryManageGrimoires(ctx context.Context, conn remoteexec.Connection, names []string, repo, state string) (bool, string, error) {
	codex, err := sorceryCodexIndex(ctx, conn)
	if err != nil {
		return false, "", err
	}

	if repo == "*" {
		switch state {
		case "present", "latest", "absent":
			action := "add"
			var todo []string
			if state == "absent" {
				action = "remove"
				for _, n := range names {
					if _, ok := codex[n]; ok {
						todo = append(todo, n)
					}
				}
			} else {
				for _, n := range names {
					if _, ok := codex[n]; !ok {
						todo = append(todo, n)
					}
				}
			}
			verb := action
			if len(verb) > 5 {
				verb = verb[:5]
			}
			if len(todo) == 0 {
				return false, fmt.Sprintf("all grimoire(s) are already %sed", verb), nil
			}
			cmd := "scribe " + action + " " + quoteAll(todo)
			if _, err := run(ctx, conn, cmd); err != nil {
				return false, "", fmt.Errorf("sorcery: failed to %s one or more grimoire(s): %w", action, err)
			}
			return true, fmt.Sprintf("successfully %sed one or more grimoire(s)", verb), nil
		default:
			return false, "", errArg("sorcery: unsupported operation on '*' repository value")
		}
	}

	switch state {
	case "present", "latest":
		if len(names) > 1 {
			return false, "", errArg("sorcery: using multiple items with repository is invalid")
		}
		grimoire := names[0]
		if _, ok := codex[grimoire]; ok {
			return false, fmt.Sprintf("grimoire %s already exists", grimoire), nil
		}
		cmd := "scribe add " + shellQuote(grimoire) + " from " + shellQuote(repo)
		if _, err := run(ctx, conn, cmd); err != nil {
			return false, "", fmt.Errorf("sorcery: failed to add grimoire %s from %s: %w", grimoire, repo, err)
		}
		return true, fmt.Sprintf("successfully added grimoire %s from %s", grimoire, repo), nil
	default:
		return false, "", errArg("sorcery: unsupported operation on repository value")
	}
}

// sorceryUpdateCodex runs `scribe update`, scoped to `names` when repo
// is set, unless every relevant grimoire's lastupdate stamp is already
// fresher than cacheValidTime seconds.
func sorceryUpdateCodex(ctx context.Context, conn remoteexec.Connection, names []string, repo string, cacheValidTime int) (bool, string, error) {
	scope := names
	if repo == "" {
		codex, err := sorceryCodexIndex(ctx, conn)
		if err != nil {
			return false, "", err
		}
		scope = nil
		for g := range codex {
			scope = append(scope, g)
		}
	}

	fresh, err := sorceryCodexFresh(ctx, conn, scope, cacheValidTime)
	if err != nil {
		return false, "", err
	}
	if fresh {
		return false, "successfully updated Codex", nil
	}

	cmd := "scribe update"
	if repo != "" {
		cmd += " " + quoteAll(names)
	}
	if _, err := run(ctx, conn, cmd); err != nil {
		return false, "", fmt.Errorf("sorcery: unable to update Codex: %w", err)
	}
	return true, "successfully updated Codex", nil
}

// sorceryCodexFresh reports whether every grimoire in names has a
// `/var/state/sorcery/<grimoire>.lastupdate` mtime within the last
// validSeconds seconds. validSeconds<=0 or an empty names always
// reports not-fresh (i.e. always update), matching real sorcery's own
// codex_fresh(), which never treats a cache_valid_time of 0 as valid.
func sorceryCodexFresh(ctx context.Context, conn remoteexec.Connection, names []string, validSeconds int) (bool, error) {
	if validSeconds <= 0 || len(names) == 0 {
		return false, nil
	}
	var checks []string
	for _, g := range names {
		f := shellQuote("/var/state/sorcery/" + g + ".lastupdate")
		checks = append(checks, fmt.Sprintf(
			`{ mt=$(stat -c %%Y %s 2>/dev/null || stat -f %%m %s 2>/dev/null) && [ -n "$mt" ] && [ $((mt + %d)) -ge $(date +%%s) ]; }`,
			f, f, validSeconds,
		))
	}
	res, err := runStatus(ctx, conn, strings.Join(checks, " && "))
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

// sorceryGazeVersions parses `gaze -q version <spells>` output into
// spell -> [grimoire_version, installed_version] ("-" for
// installed_version means not installed), matching real sorcery's own
// pipe-delimited "...|...|spell|grim_ver|inst_ver" row format.
func sorceryGazeVersions(out string) map[string][2]string {
	result := map[string][2]string{}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 5 {
			continue
		}
		spell := strings.TrimSpace(parts[2])
		grimVer := strings.TrimSpace(parts[3])
		instVer := strings.TrimSpace(parts[4])
		if spell == "" {
			continue
		}
		result[spell] = [2]string{grimVer, instVer}
	}
	return result
}

func sorceryManageSpells(ctx context.Context, conn remoteexec.Connection, names []string, state, depends string) (bool, string, error) {
	if len(names) == 1 && names[0] == "*" {
		switch state {
		case "latest":
			if _, err := run(ctx, conn, "sorcery queue"); err != nil {
				return false, "", fmt.Errorf("sorcery: failed to generate the update queue: %w", err)
			}
			out, err := run(ctx, conn, "stat -c %s /var/log/sorcery/queue/install 2>/dev/null || stat -f %z /var/log/sorcery/queue/install 2>/dev/null || echo 0")
			if err != nil {
				return false, "", fmt.Errorf("sorcery: failed to read the update queue: %w", err)
			}
			size, _ := strconv.Atoi(strings.TrimSpace(out))
			if size == 0 {
				return false, "the system is already up to date", nil
			}
			if _, err := run(ctx, conn, "cast --queue"); err != nil {
				return false, "", fmt.Errorf("sorcery: failed to update the system: %w", err)
			}
			return true, "successfully updated the system", nil
		case "rebuild":
			if _, err := run(ctx, conn, "sorcery rebuild"); err != nil {
				return false, "", fmt.Errorf("sorcery: failed to rebuild the system: %w", err)
			}
			return true, "successfully rebuilt the system", nil
		default:
			return false, "", errArg("sorcery: unsupported operation on '*' name value")
		}
	}

	gazeOut, err := run(ctx, conn, "gaze -q version "+quoteAll(names))
	if err != nil {
		return false, "", fmt.Errorf("sorcery: failed to locate spell(s) in the list (%s): %w", strings.Join(names, ", "), err)
	}
	versions := sorceryGazeVersions(gazeOut)

	var castQueue, dispelQueue []string
	for _, spell := range names {
		grimVer, instVer := "", "-"
		if v, ok := versions[spell]; ok {
			grimVer, instVer = v[0], v[1]
		}
		switch state {
		case "present":
			if instVer == "-" {
				castQueue = append(castQueue, spell)
			}
		case "latest":
			if grimVer != instVer {
				castQueue = append(castQueue, spell)
			}
		case "rebuild":
			castQueue = append(castQueue, spell)
		case "absent":
			if instVer != "-" {
				dispelQueue = append(dispelQueue, spell)
			}
		}
	}

	if depends != "" && len(names) == 1 {
		if err := sorceryApplyDepends(ctx, conn, names[0], depends); err != nil {
			return false, "", err
		}
		if state != "absent" && len(castQueue) == 0 {
			castQueue = append(castQueue, names[0])
		}
	}

	changed := false
	var msgs []string

	if len(castQueue) > 0 {
		if _, err := run(ctx, conn, "cast -c "+quoteAll(castQueue)); err != nil {
			return false, "", fmt.Errorf("sorcery: failed to cast spell(s): %w", err)
		}
		changed = true
		msgs = append(msgs, "successfully cast spell(s)")
	} else if state != "absent" {
		msgs = append(msgs, "spell(s) are already cast")
	}

	if state == "absent" {
		if len(dispelQueue) > 0 {
			if _, err := run(ctx, conn, "dispel "+quoteAll(dispelQueue)); err != nil {
				return false, "", fmt.Errorf("sorcery: failed to dispel spell(s): %w", err)
			}
			changed = true
			msgs = append(msgs, "successfully dispelled spell(s)")
		} else {
			msgs = append(msgs, "spell(s) are already dispelled")
		}
	}

	return changed, strings.Join(msgs, "; "), nil
}

// sorceryApplyDepends appends one `spell:dep:on|off:optional::` line
// per depends token to /var/state/sorcery/depends. See the package doc
// comment for how this differs from real sorcery's own
// validate-then-dedup approach.
func sorceryApplyDepends(ctx context.Context, conn remoteexec.Connection, spell, depends string) error {
	for _, tok := range strings.Split(depends, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		status := "on"
		switch {
		case strings.HasPrefix(tok, "+"):
			tok = tok[1:]
		case strings.HasPrefix(tok, "-"):
			tok = tok[1:]
			status = "off"
		}
		line := fmt.Sprintf("%s:%s:%s:optional::", spell, tok, status)
		cmd := "printf '%s\\n' " + shellQuote(line) + " >> /var/state/sorcery/depends"
		if _, err := run(ctx, conn, cmd); err != nil {
			return fmt.Errorf("sorcery: writing depends entry for %s: %w", spell, err)
		}
	}
	return nil
}
