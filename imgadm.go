package modules

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleImgadm implements Ansible's `imgadm` (community.general)
// module: manages SmartOS virtual machine images and image sources via
// `imgadm(8)`.
//
// Args: source (string, optional) — when set, `imgadm` manages a
// SOURCE (not an image): state present/imported adds it (`imgadm
// sources -a <source> -t <type>`), any other state removes it
// (`imgadm sources -d <source>`); type ("imgapi"|"docker"|"dsapi",
// default "imgapi"); force (bool, optional) — `-f` on the `sources`
// invocation; uuid (string, optional when managing sources; a full
// UUID or "*" otherwise) — validated against the standard
// 8-4-4-4-12 hex UUID shape unless it is "*"; pool (string, default
// "zones") — `-P` on import/delete; state (required — present,
// absent, deleted (alias of absent), imported (alias of present),
// updated, vacuumed). uuid="*" is only accepted with state=updated or
// state=vacuumed, matching real imgadm's own documented restriction.
//
// Idempotency is read from `imgadm`'s own stdout/stderr wording (this
// port has no other signal — imgadm(8) itself has no dry-run mode, the
// same limitation real imgadm's own module documents by shipping with
// supports_check_mode=False): "is already installed, skipping" or a
// stderr ActiveImageNotFound mean an import was a no-op; a last stdout
// line starting "Imported image" means it was not; a stderr
// ImageNotInstalled means a delete was a no-op; stdout containing
// "Deleted image" means it was not. state=updated has no such signal
// at all (imgadm(8) never reports whether an update changed anything)
// and is always reported changed, matching real imgadm's own module
// exactly (its own comment: "There is no feedback from imgadm(8) to
// determine if anything was actually changed").
//
// Deviation from real imgadm: real imgadm's own manage_sources(),
// on the source-REMOVAL path, builds its command as `cmd = cmd + ["-f"]`
// (a list) followed later by `cmd += f" -d {source}"` — adding a
// Python STRING to a LIST via `+=` does not append one "-d source"
// token, it extends the list with the string's individual CHARACTERS
// (`'-'`, `' '`, `'d'`, `' '`, ...). This reads as a genuine bug in
// real imgadm's own module (nothing about it is documented or
// intentional-looking) that would corrupt every real "remove a
// source" invocation. This port instead appends "-d", source as the
// two argv tokens the surrounding code obviously means to produce.
// Separately, real imgadm's own manage_images() runs its
// `state == "vacuumed"` branch and then UNCONDITIONALLY falls through
// into `if self.present: ... else: ...` afterward (these are not
// `elif`s) — and since self.present is only true for
// present/imported/updated, state=vacuumed always takes the `else`
// (delete) branch too, running `imgadm delete -P pool *` right after
// the vacuum whenever uuid="*" (the only uuid value real imgadm's own
// module accepts alongside state=vacuumed). Nothing in imgadm's own
// OPTIONS or EXAMPLES suggests a vacuum should also attempt a mass
// delete, so this reads as an unintended side effect of real imgadm's
// own control flow rather than documented intent; this port runs only
// the vacuum step for state=vacuumed.
func moduleImgadm(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	switch state {
	case "present", "absent", "deleted", "imported", "updated", "vacuumed":
	default:
		return Result{}, errArg("imgadm: state must be one of present, absent, deleted, imported, updated, vacuumed, got %q", state)
	}
	force := argBool(args, "force", false)
	pool := argString(args, "pool", "zones")
	source := argString(args, "source", "")
	imgType := argString(args, "type", "imgapi")
	uuid := argString(args, "uuid", "")

	if uuid != "" && uuid != "*" && !imgadmUUIDRE.MatchString(uuid) {
		return Result{}, errArg("imgadm: uuid %q is not a valid UUID", uuid)
	}

	present := state == "present" || state == "imported" || state == "updated"

	if source != "" {
		changed, err := imgadmManageSource(ctx, conn, source, imgType, force, present)
		if err != nil {
			return Result{}, err
		}
		r := Result{Changed: changed}
		r = r.WithExtra("source", source).WithExtra("state", state)
		return r, nil
	}

	if uuid == "*" && state != "updated" && state != "vacuumed" {
		return Result{}, errArg(`imgadm: can only specify uuid as "*" when updating image(s)`)
	}

	var changed bool
	switch state {
	case "updated":
		changed, err = imgadmUpdateImages(ctx, conn, uuid)
	case "vacuumed":
		changed, err = imgadmVacuum(ctx, conn)
	default:
		changed, err = imgadmManageImage(ctx, conn, pool, uuid, present)
	}
	if err != nil {
		return Result{}, err
	}
	r := Result{Changed: changed}
	r = r.WithExtra("uuid", uuid).WithExtra("state", state)
	return r, nil
}

var imgadmUUIDRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)
var imgadmErrRE = regexp.MustCompile(`^imgadm .*?: error \(\w+\): (.*?): .*`)

func imgadmErrMsg(stderr string) string {
	if m := imgadmErrRE.FindStringSubmatch(stderr); m != nil {
		return m[1]
	}
	return "Unexpected failure"
}

func imgadmManageSource(ctx context.Context, conn remoteexec.Connection, source, imgType string, force, present bool) (bool, error) {
	base := []string{"imgadm", "sources"}
	if force {
		base = append(base, "-f")
	}
	if present {
		tokens := append(append([]string{}, base...), "-a", source, "-t", imgType)
		res, err := runStatus(ctx, conn, quoteAll(tokens))
		if err != nil {
			return false, err
		}
		if res.RC != 0 {
			return false, fmt.Errorf("imgadm: failed to add source: %s", imgadmErrMsg(res.Stderr))
		}
		if strings.Contains(res.Stdout, fmt.Sprintf(`Already have "%s" image source "%s"`, imgType, source)) {
			return false, nil
		}
		return strings.Contains(res.Stdout, fmt.Sprintf(`Added "%s" image source "%s"`, imgType, source)), nil
	}
	tokens := append(append([]string{}, base...), "-d", source)
	res, err := runStatus(ctx, conn, quoteAll(tokens))
	if err != nil {
		return false, err
	}
	if res.RC != 0 {
		return false, fmt.Errorf("imgadm: failed to remove source: %s", imgadmErrMsg(res.Stderr))
	}
	if strings.Contains(res.Stdout, fmt.Sprintf(`Do not have image source "%s", no change`, source)) {
		return false, nil
	}
	return strings.Contains(res.Stdout, "Deleted") && strings.Contains(res.Stdout, fmt.Sprintf(`image source "%s"`, source)), nil
}

func imgadmUpdateImages(ctx context.Context, conn remoteexec.Connection, uuid string) (bool, error) {
	tokens := []string{"imgadm", "update"}
	if uuid != "" && uuid != "*" {
		tokens = append(tokens, uuid)
	}
	res, err := runStatus(ctx, conn, quoteAll(tokens))
	if err != nil {
		return false, err
	}
	if res.RC != 0 {
		return false, fmt.Errorf("imgadm: failed to update images: %s", imgadmErrMsg(res.Stderr))
	}
	return true, nil
}

func imgadmVacuum(ctx context.Context, conn remoteexec.Connection) (bool, error) {
	res, err := runStatus(ctx, conn, "imgadm vacuum -f")
	if err != nil {
		return false, err
	}
	if res.RC != 0 {
		return false, fmt.Errorf("imgadm: failed to vacuum images: %s", imgadmErrMsg(res.Stderr))
	}
	return strings.TrimSpace(res.Stdout) != "", nil
}

func imgadmManageImage(ctx context.Context, conn remoteexec.Connection, pool, uuid string, present bool) (bool, error) {
	if present {
		tokens := []string{"imgadm", "import", "-P", pool, "-q"}
		if uuid != "" {
			tokens = append(tokens, uuid)
		}
		res, err := runStatus(ctx, conn, quoteAll(tokens))
		if err != nil {
			return false, err
		}
		if res.RC != 0 {
			return false, fmt.Errorf("imgadm: failed to import image: %s", imgadmErrMsg(res.Stderr))
		}
		if strings.Contains(res.Stdout, "is already installed, skipping") {
			return false, nil
		}
		if strings.Contains(res.Stderr, "ActiveImageNotFound") {
			return false, nil
		}
		lines := strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n")
		return len(lines) > 0 && strings.HasPrefix(lines[len(lines)-1], "Imported image"), nil
	}

	tokens := []string{"imgadm", "delete", "-P", pool}
	if uuid != "" {
		tokens = append(tokens, uuid)
	}
	res, err := runStatus(ctx, conn, quoteAll(tokens))
	if err != nil {
		return false, err
	}
	if strings.Contains(res.Stderr, "ImageNotInstalled") {
		return false, nil
	}
	return strings.Contains(res.Stdout, "Deleted image"), nil
}
