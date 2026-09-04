package modules

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSolarisZone implements (a subset of) Ansible's `solaris_zone`
// module: manages a Solaris zone's full lifecycle through `zonecfg(1M)`
// and `zoneadm(1M)`.
//
// Args: name (string, required) — must match `^[a-zA-Z0-9][-_.a-zA-Z0-9]{0,62}$`,
// exactly real solaris_zone's own validation regex. state (absent|
// attached|configured|detached|installed|present|running|started|
// stopped, default "present") — see the state table in real
// solaris_zone's own OPTIONS, reproduced by this port's own state
// handlers below (present/installed, running/started, stopped, absent,
// configured, detached, attached — including present/installed's own
// idempotent "already exists" no-op, and attached's own real (slightly
// confusing) quirk of appending "zone already attached" even when the
// zone was reported as not existing — real state_attached() has no
// `return`/`else` after that first message, so it always falls through
// to the is_configured() check, which is false for a nonexistent zone,
// landing on the "already attached" branch; this port replicates that
// exactly rather than "fixing" it). path (string, required for
// configure) — zonepath. sparse (bool, default false) — sparse-root
// (`zonecfg create`) vs whole-root (`zonecfg create -b`). root_password
// (string, optional) — root's crypt(3) hash, written into the new
// zone's own /etc/shadow after install. config (string, default "") —
// raw zonecfg(1M) commands appended verbatim to the create script.
// create_options/install_options/attach_options (string, default "")
// — extra CLI args to zonecfg create / zoneadm install / zoneadm
// attach, respectively, appended only when non-empty (real
// solaris_zone always appends its own install_options/attach_options
// even when empty, passing a literal empty argv entry to zoneadm — see
// Simplifications below for why this port omits it instead). timeout
// (int, default 600) — seconds to wait for `ps -z <name> -o args` to
// show a console login (`ttymon.*-d /dev/console`) after `zoneadm boot`,
// matching real solaris_zone's own boot() polling loop exactly
// (10-second intervals).
//
// This module verifies the TARGET is actually Solaris (`uname -s` ==
// "SunOS") and Solaris 10+ (`uname -r`'s minor version), matching real
// solaris_zone's own platform.system()/platform.release() checks —
// but, unlike real solaris_zone (which crashes at Python import time
// on a non-Solaris CONTROLLER, since it runs its whole Python process
// there), this port's checks run ON THE TARGET via conn.Exec, which is
// architecturally the more correct place for them in this port's own
// model (a Connection reached via Exec, not a script copied to and
// executed on the target).
//
// Simplifications vs real solaris_zone, disclosed rather than silently
// dropped:
//
//   - Real solaris_zone's own install() step, after `zoneadm install`,
//     also (a) on Solaris 10 only, hand-writes several sysidtool
//     "already configured" answer files under
//     <path>/root/etc/.sysIDtool.state (and deletes
//     <path>/root/etc/.UNCONFIGURED, and writes root/nodename) so the
//     zone's first boot skips Solaris 10's interactive system-
//     identification prompts, and (b) always pre-generates SSH host
//     keys under <path>/root/etc/ssh/ via `ssh-keygen`, so a fresh
//     zone doesn't regenerate them (and doesn't need sshd to do it) on
//     first start. This port does NEITHER: a Solaris 10 zone installed
//     by this port may still prompt interactively for system
//     identification on first console boot (this port's own boot()
//     polling loop, which waits for a console login prompt, will still
//     eventually observe ttymon and return — it does not itself
//     interact with any sysid prompt), and SSH host keys are left for
//     sshd itself to generate on first start, which is normal sshd
//     behavior on essentially every modern system and not itself
//     broken, just not pre-seeded the way real solaris_zone pre-seeds
//     it.
//   - install_options/attach_options are appended to their zoneadm
//     command only when non-empty; real solaris_zone appends them
//     unconditionally (even as a literal empty string argv entry) —
//     this port's shell-command composition has no equivalent of an
//     "empty but present" argv entry worth reproducing here (it would
//     just be a stray blank token most implementations of zoneadm are
//     no more likely to accept than to reject), so it is omitted
//     instead, a deliberate, documented improvement rather than a
//     faithful-but-pointless replication of an argv artifact.
//
// This port has no check_mode support at all (a runtime-engine concern
// outside every module's own Func signature here, not specific to this
// module), unlike real solaris_zone which supports it.
func moduleSolarisZone(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	if !solarisZoneNameRE.MatchString(name) {
		return Fail("solaris_zone: provided zone name is not a valid zone name. " +
			"Please refer documentation for correct zone name specifications."), nil
	}
	state := argString(args, "state", "present")
	switch state {
	case "absent", "attached", "configured", "detached", "installed", "present", "running", "started", "stopped":
	default:
		return Result{}, errArg("solaris_zone: invalid state: %q", state)
	}
	z := &solarisZoneCtx{
		conn:           conn,
		name:           name,
		path:           argString(args, "path", ""),
		sparse:         argBool(args, "sparse", false),
		rootPassword:   argString(args, "root_password", ""),
		config:         argString(args, "config", ""),
		createOptions:  argString(args, "create_options", ""),
		installOptions: argString(args, "install_options", ""),
		attachOptions:  argString(args, "attach_options", ""),
		timeout:        argInt(args, "timeout", 600),
	}

	if failMsg, err := z.checkPlatform(ctx); err != nil {
		return Result{}, err
	} else if failMsg != "" {
		return Fail(failMsg), nil
	}

	var msgs []string
	var failMsg string
	switch state {
	case "running", "started":
		msgs, failMsg, err = z.stateRunning(ctx)
	case "present", "installed":
		msgs, failMsg, err = z.statePresent(ctx)
	case "stopped":
		msgs, failMsg, err = z.stateStopped(ctx)
	case "absent":
		msgs, failMsg, err = z.stateAbsent(ctx)
	case "configured":
		msgs, failMsg, err = z.stateConfigured(ctx)
	case "detached":
		msgs, failMsg, err = z.stateDetached(ctx)
	case "attached":
		msgs, failMsg, err = z.stateAttached(ctx)
	}
	if err != nil {
		return Result{}, err
	}
	if failMsg != "" {
		return Fail(failMsg), nil
	}

	result := Ok(strings.Join(msgs, ", "))
	if z.changed {
		result = Changed(strings.Join(msgs, ", "))
	}
	return result, nil
}

var solarisZoneNameRE = regexp.MustCompile(`^[a-zA-Z0-9][-_.a-zA-Z0-9]{0,62}$`)

type solarisZoneCtx struct {
	conn remoteexec.Connection
	name string

	path           string
	sparse         bool
	rootPassword   string
	config         string
	createOptions  string
	installOptions string
	attachOptions  string
	timeout        int

	changed bool
}

// checkPlatform runs `uname -s -r` once and validates both that the
// target is SunOS (matching real solaris_zone's own
// `platform.system() != "SunOS"` check) and that its release is 5.10+
// (Solaris's own `uname -r` reports "5.10"/"5.11" for Solaris 10/11 —
// real solaris_zone's own `platform.release().split(".")` reads the
// exact same value via Python's platform module), matching real
// solaris_zone's own two separate checks in one round trip.
func (z *solarisZoneCtx) checkPlatform(ctx context.Context) (failMsg string, err error) {
	out, err := run(ctx, z.conn, "uname -s -r")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	if len(fields) < 2 || fields[0] != "SunOS" {
		return "solaris_zone: this module requires Solaris", nil
	}
	relFields := strings.SplitN(fields[1], ".", 2)
	if len(relFields) < 2 {
		return "solaris_zone: this module requires Solaris 10 or later", nil
	}
	minor, err2 := strconv.Atoi(relFields[1])
	if err2 != nil || minor < 10 {
		return "solaris_zone: this module requires Solaris 10 or later", nil
	}
	return "", nil
}

// exists reports whether the zone is known to zoneadm at all, matching
// real solaris_zone's own exists(): `zoneadm -z <name> list`, rc == 0.
func (z *solarisZoneCtx) exists(ctx context.Context) (bool, error) {
	res, err := runStatus(ctx, z.conn, "zoneadm -z "+shellQuote(z.name)+" list")
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

// status returns the zone's own state field ("configured"/"installed"/
// "running"/...) from `zoneadm -z <name> list -p`'s third colon-
// separated field, or "undefined" on failure — matching real
// solaris_zone's own status(): `out.split(":")[2]`.
func (z *solarisZoneCtx) status(ctx context.Context) (string, error) {
	res, err := runStatus(ctx, z.conn, "zoneadm -z "+shellQuote(z.name)+" list -p")
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "undefined", nil
	}
	fields := strings.Split(strings.TrimSpace(res.Stdout), ":")
	if len(fields) < 3 {
		return "undefined", nil
	}
	return fields[2], nil
}

func (z *solarisZoneCtx) isRunning(ctx context.Context) (bool, error) {
	s, err := z.status(ctx)
	return s == "running", err
}

func (z *solarisZoneCtx) isInstalled(ctx context.Context) (bool, error) {
	s, err := z.status(ctx)
	return s == "installed", err
}

func (z *solarisZoneCtx) isConfigured(ctx context.Context) (bool, error) {
	s, err := z.status(ctx)
	return s == "configured", err
}

// configure writes a zonecfg(1M) create script to a temp file on the
// target and applies it via `zonecfg -z <name> -f <file>`, matching
// real solaris_zone's own configure(): a `create [-b] <create_options>`
// line, a `set zonepath=<path>` line, then `config` verbatim.
func (z *solarisZoneCtx) configure(ctx context.Context) (msg, failMsg string, err error) {
	if z.path == "" {
		return "", "solaris_zone: missing required argument: path", nil
	}

	var script strings.Builder
	if z.sparse {
		script.WriteString("create " + z.createOptions + "\n")
	} else {
		script.WriteString("create -b " + z.createOptions + "\n")
	}
	script.WriteString("set zonepath=" + z.path + "\n")
	script.WriteString(z.config + "\n")

	tmp := z.conn.TempPath("solaris_zone-" + z.name + ".zonecfg")
	writeCmd := "cat > " + shellQuote(tmp) + " <<'SOLARIS_ZONE_EOF'\n" + script.String() + "\nSOLARIS_ZONE_EOF\n"
	if _, err := run(ctx, z.conn, writeCmd); err != nil {
		return "", "", fmt.Errorf("solaris_zone: failed to write zonecfg script: %w", err)
	}
	defer z.conn.Remove(ctx, tmp)

	res, err := runStatus(ctx, z.conn, "zonecfg -z "+shellQuote(z.name)+" -f "+shellQuote(tmp))
	if err != nil {
		return "", "", err
	}
	if res.RC != 0 {
		return "", fmt.Sprintf("solaris_zone: failed to create zone. %s", strings.TrimSpace(res.Stdout+res.Stderr)), nil
	}
	z.changed = true
	return "zone configured", "", nil
}

// install runs `zoneadm -z <name> install [install_options]`, matching
// real solaris_zone's own install() — minus the sysid/ssh-host-key
// bootstrap steps documented as omitted in moduleSolarisZone's own doc
// comment.
func (z *solarisZoneCtx) install(ctx context.Context) (failMsg string, err error) {
	cmd := "zoneadm -z " + shellQuote(z.name) + " install"
	if z.installOptions != "" {
		cmd += " " + z.installOptions
	}
	res, err := runStatus(ctx, z.conn, cmd)
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return fmt.Sprintf("solaris_zone: failed to install zone. %s", strings.TrimSpace(res.Stdout+res.Stderr)), nil
	}
	if z.rootPassword != "" {
		if err := z.configurePassword(ctx); err != nil {
			return "", err
		}
	}
	z.changed = true
	return "", nil
}

// configurePassword rewrites root's password-hash field in the newly
// installed zone's own /etc/shadow (<path>/root/etc/shadow, visible
// from the global zone), matching real solaris_zone's own
// configure_password(). This port uses `awk` (field-safe against any
// character in the hash except a literal backslash-escape sequence
// awk's own -v assignment interprets, e.g. "\n" — a real, narrow
// limitation for a crypt(3) hash containing a backslash, which does
// not occur in the glibc/Solaris crypt algorithms this hash is
// expected to come from) rather than real solaris_zone's own Python
// line-rewrite, to avoid needing a regex-escaped sed substitution
// against arbitrary hash content (slashes/dots/dollar signs are all
// routine in a crypt(3) hash and would need per-character escaping in
// sed but not in awk -v).
func (z *solarisZoneCtx) configurePassword(ctx context.Context) error {
	shadow := z.path + "/root/etc/shadow"
	cmd := "awk -F: -v OFS=: -v h=" + shellQuote(z.rootPassword) +
		` '$1=="root"{$2=h} {print}' ` + shellQuote(shadow) + " > " + shellQuote(shadow+".new") +
		" && mv " + shellQuote(shadow+".new") + " " + shellQuote(shadow)
	if _, err := run(ctx, z.conn, cmd); err != nil {
		return fmt.Errorf("solaris_zone: failed to set root password: %w", err)
	}
	return nil
}

// uninstall runs `zoneadm -z <name> uninstall -F` if the zone is
// currently installed, matching real solaris_zone's own uninstall().
func (z *solarisZoneCtx) uninstall(ctx context.Context) (failMsg string, err error) {
	installed, err := z.isInstalled(ctx)
	if err != nil {
		return "", err
	}
	if !installed {
		return "", nil
	}
	res, err := runStatus(ctx, z.conn, "zoneadm -z "+shellQuote(z.name)+" uninstall -F")
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return fmt.Sprintf("solaris_zone: failed to uninstall zone. %s", strings.TrimSpace(res.Stdout+res.Stderr)), nil
	}
	z.changed = true
	return "", nil
}

// boot runs `zoneadm -z <name> boot`, then polls `ps -z <name> -o args`
// every 10 seconds (up to timeout) for a console-login process
// (`ttymon.*-d /dev/console`), matching real solaris_zone's own boot()
// exactly, including its own 10-second poll interval.
func (z *solarisZoneCtx) boot(ctx context.Context) (failMsg string, err error) {
	res, err := runStatus(ctx, z.conn, "zoneadm -z "+shellQuote(z.name)+" boot")
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return fmt.Sprintf("solaris_zone: failed to boot zone. %s", strings.TrimSpace(res.Stdout+res.Stderr)), nil
	}

	bootedRE := regexp.MustCompile(`ttymon.*-d /dev/console`)
	elapsed := 0
	for {
		if elapsed > z.timeout {
			return "solaris_zone: timed out waiting for zone to boot", nil
		}
		psRes, err := runStatus(ctx, z.conn, "ps -z "+shellQuote(z.name)+" -o args")
		if err != nil {
			return "", err
		}
		booted := false
		for _, line := range strings.Split(psRes.Stdout, "\n") {
			if bootedRE.MatchString(line) {
				booted = true
				break
			}
		}
		if booted {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(10 * time.Second):
		}
		elapsed += 10
	}
	z.changed = true
	return "", nil
}

func (z *solarisZoneCtx) stop(ctx context.Context) (failMsg string, err error) {
	res, err := runStatus(ctx, z.conn, "zoneadm -z "+shellQuote(z.name)+" halt")
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return fmt.Sprintf("solaris_zone: failed to stop zone. %s", strings.TrimSpace(res.Stdout+res.Stderr)), nil
	}
	z.changed = true
	return "", nil
}

func (z *solarisZoneCtx) detach(ctx context.Context) (failMsg string, err error) {
	res, err := runStatus(ctx, z.conn, "zoneadm -z "+shellQuote(z.name)+" detach")
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return fmt.Sprintf("solaris_zone: failed to detach zone. %s", strings.TrimSpace(res.Stdout+res.Stderr)), nil
	}
	z.changed = true
	return "", nil
}

func (z *solarisZoneCtx) attach(ctx context.Context) (failMsg string, err error) {
	cmd := "zoneadm -z " + shellQuote(z.name) + " attach"
	if z.attachOptions != "" {
		cmd += " " + z.attachOptions
	}
	res, err := runStatus(ctx, z.conn, cmd)
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return fmt.Sprintf("solaris_zone: failed to attach zone. %s", strings.TrimSpace(res.Stdout+res.Stderr)), nil
	}
	z.changed = true
	return "", nil
}

func (z *solarisZoneCtx) destroy(ctx context.Context) (msgs []string, failMsg string, err error) {
	running, err := z.isRunning(ctx)
	if err != nil {
		return nil, "", err
	}
	if running {
		if failMsg, err = z.stop(ctx); err != nil || failMsg != "" {
			return nil, failMsg, err
		}
		msgs = append(msgs, "zone stopped")
	}
	if failMsg, err = z.uninstall(ctx); err != nil || failMsg != "" {
		return nil, failMsg, err
	}
	res, err := runStatus(ctx, z.conn, "zonecfg -z "+shellQuote(z.name)+" delete -F")
	if err != nil {
		return nil, "", err
	}
	if res.RC != 0 {
		return nil, fmt.Sprintf("solaris_zone: failed to delete zone. %s", strings.TrimSpace(res.Stdout+res.Stderr)), nil
	}
	z.changed = true
	msgs = append(msgs, "zone deleted")
	return msgs, "", nil
}

// --- state_* handlers, one per moduleSolarisZone's own `state` value ---

func (z *solarisZoneCtx) statePresent(ctx context.Context) (msgs []string, failMsg string, err error) {
	exists, err := z.exists(ctx)
	if err != nil {
		return nil, "", err
	}
	if exists {
		return []string{"zone already exists"}, "", nil
	}
	cfgMsg, failMsg, err := z.configure(ctx)
	if err != nil || failMsg != "" {
		return nil, failMsg, err
	}
	msgs = append(msgs, cfgMsg)
	if failMsg, err = z.install(ctx); err != nil || failMsg != "" {
		return nil, failMsg, err
	}
	msgs = append(msgs, "zone installed")
	return msgs, "", nil
}

func (z *solarisZoneCtx) stateRunning(ctx context.Context) (msgs []string, failMsg string, err error) {
	msgs, failMsg, err = z.statePresent(ctx)
	if err != nil || failMsg != "" {
		return nil, failMsg, err
	}
	running, err := z.isRunning(ctx)
	if err != nil {
		return nil, "", err
	}
	if running {
		msgs = append(msgs, "zone already running")
		return msgs, "", nil
	}
	if failMsg, err = z.boot(ctx); err != nil || failMsg != "" {
		return nil, failMsg, err
	}
	msgs = append(msgs, "zone booted")
	return msgs, "", nil
}

func (z *solarisZoneCtx) stateStopped(ctx context.Context) (msgs []string, failMsg string, err error) {
	exists, err := z.exists(ctx)
	if err != nil {
		return nil, "", err
	}
	if !exists {
		return nil, "solaris_zone: zone does not exist", nil
	}
	if failMsg, err = z.stop(ctx); err != nil || failMsg != "" {
		return nil, failMsg, err
	}
	return []string{"zone stopped"}, "", nil
}

func (z *solarisZoneCtx) stateAbsent(ctx context.Context) (msgs []string, failMsg string, err error) {
	exists, err := z.exists(ctx)
	if err != nil {
		return nil, "", err
	}
	if !exists {
		return []string{"zone does not exist"}, "", nil
	}
	return z.destroy(ctx)
}

func (z *solarisZoneCtx) stateConfigured(ctx context.Context) (msgs []string, failMsg string, err error) {
	exists, err := z.exists(ctx)
	if err != nil {
		return nil, "", err
	}
	if exists {
		return []string{"zone already exists"}, "", nil
	}
	cfgMsg, failMsg, err := z.configure(ctx)
	if err != nil || failMsg != "" {
		return nil, failMsg, err
	}
	return []string{cfgMsg}, "", nil
}

func (z *solarisZoneCtx) stateDetached(ctx context.Context) (msgs []string, failMsg string, err error) {
	exists, err := z.exists(ctx)
	if err != nil {
		return nil, "", err
	}
	if !exists {
		return nil, "solaris_zone: zone does not exist", nil
	}
	configured, err := z.isConfigured(ctx)
	if err != nil {
		return nil, "", err
	}
	if configured {
		return []string{"zone already detached"}, "", nil
	}
	if failMsg, err = z.stop(ctx); err != nil || failMsg != "" {
		return nil, failMsg, err
	}
	if failMsg, err = z.detach(ctx); err != nil || failMsg != "" {
		return nil, failMsg, err
	}
	return []string{"zone stopped", "zone detached"}, "", nil
}

// stateAttached replicates real solaris_zone's own state_attached()
// verbatim, including its own quirk: there is no early return when the
// zone does not exist, so execution always falls through to the
// is_configured() check — which is false ("undefined") for a
// nonexistent zone — landing on the "zone already attached" message
// even though the zone was JUST reported as not existing. This is real
// solaris_zone's own behavior, not a bug introduced by this port; see
// moduleSolarisZone's own doc comment.
func (z *solarisZoneCtx) stateAttached(ctx context.Context) (msgs []string, failMsg string, err error) {
	exists, err := z.exists(ctx)
	if err != nil {
		return nil, "", err
	}
	if !exists {
		msgs = append(msgs, "zone does not exist")
	}
	configured, err := z.isConfigured(ctx)
	if err != nil {
		return nil, "", err
	}
	if configured {
		if failMsg, err = z.attach(ctx); err != nil || failMsg != "" {
			return nil, failMsg, err
		}
		msgs = append(msgs, "zone attached")
		return msgs, "", nil
	}
	msgs = append(msgs, "zone already attached")
	return msgs, "", nil
}
