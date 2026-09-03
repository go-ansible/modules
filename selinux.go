package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSelinux implements (a subset of) Ansible's `selinux` module:
// rewrites the SELinux config file's `SELINUX=`/`SELINUXTYPE=` keys and,
// where possible, applies the mode live via `setenforce`.
//
// Args: state (enforcing|permissive|disabled, required); policy
// (string, required unless state=disabled); configfile (string,
// default "/etc/selinux/config").
//
// A reboot may be required after this module runs, exactly as real
// selinux documents: switching state TO or FROM "disabled" cannot take
// effect live (SELinux is a kernel-boot-time decision at that point) —
// `setenforce` only toggles between enforcing and permissive on an
// already-ENABLED kernel. This module always rewrites configfile
// immediately (so the change is durable across a reboot the caller
// arranges separately, matching reboot.go's own "this port cannot wait
// for the host to come back" limitation) and reports whether a reboot
// is needed via Extra["reboot_required"], the same signal real selinux
// itself returns — but unlike real selinux, this port does not (and
// cannot) issue that reboot itself.
//
// This module requires configfile to already exist — it fails (a
// transport-level error from the `cat`) rather than fabricating one
// from scratch, since a hand-written minimal config risks being wrong
// in ways specific to the distro's SELinux packaging; a real narrowing
// versus real selinux, which can create the file, documented here.
// `update_kernel_param` (real selinux's `grubby`-based kernel
// boot-argument rewriting) is accepted but NOT implemented — grubby's
// own behavior is bootloader- and distro-specific enough that this
// port did not judge a shell-composed subset worth the risk of a wrong
// boot argument; a real, intentional gap, not a silent approximation.
func moduleSelinux(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	if state != "enforcing" && state != "permissive" && state != "disabled" {
		return Result{}, errArg("selinux: state must be enforcing, permissive, or disabled, got %q", state)
	}
	policy := argString(args, "policy", "")
	if state != "disabled" && policy == "" {
		return Result{}, errArg("selinux: policy is required unless state is disabled")
	}
	configfile := argString(args, "configfile", "/etc/selinux/config")

	content, err := run(ctx, conn, "cat "+shellQuote(configfile))
	if err != nil {
		return Result{}, errArg("selinux: reading %s: %v (this port requires configfile to already exist)", configfile, err)
	}
	lines := strings.Split(content, "\n")
	lines, cfgChanged1 := setConfigKV(lines, "SELINUX", state)
	cfgChanged2 := false
	if policy != "" {
		lines, cfgChanged2 = setConfigKV(lines, "SELINUXTYPE", policy)
	}
	cfgChanged := cfgChanged1 || cfgChanged2
	if cfgChanged {
		newContent := strings.Join(lines, "\n")
		if _, err := conn.Exec(ctx, "cat > "+shellQuote(configfile), strings.NewReader(newContent)); err != nil {
			return Result{}, err
		}
	}

	getRes, err := runStatus(ctx, conn, "getenforce")
	if err != nil {
		return Result{}, err
	}
	if getRes.RC != 0 {
		return Fail("selinux: getenforce not found or failed; SELinux may not be installed on this host"), nil
	}
	curLive := strings.TrimSpace(getRes.Stdout)

	liveChanged := false
	rebootRequired := false
	switch state {
	case "enforcing":
		if curLive == "Disabled" {
			rebootRequired = true
		} else if curLive != "Enforcing" {
			if _, err := run(ctx, conn, "setenforce 1"); err != nil {
				return Result{}, err
			}
			liveChanged = true
		}
	case "permissive":
		if curLive == "Disabled" {
			rebootRequired = true
		} else if curLive != "Permissive" {
			if _, err := run(ctx, conn, "setenforce 0"); err != nil {
				return Result{}, err
			}
			liveChanged = true
		}
	case "disabled":
		if curLive != "Disabled" {
			rebootRequired = true
		}
	}

	changed := cfgChanged || liveChanged
	msg := "SELinux state is " + state
	if changed {
		msg = "SELinux state changed to " + state
	}
	res := Result{Changed: changed, Msg: msg}
	res = res.WithExtra("configfile", configfile).
		WithExtra("policy", policy).
		WithExtra("state", state).
		WithExtra("reboot_required", rebootRequired)
	return res, nil
}

// setConfigKV replaces the first "key=..." line (ignoring surrounding
// whitespace, and never matching inside a comment) with "key=value",
// or appends one if no such line exists. Returns the new lines and
// whether anything changed.
func setConfigKV(lines []string, key, value string) ([]string, bool) {
	found := false
	changed := false
	out := make([]string, 0, len(lines)+1)
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), key+"=") {
			found = true
			newLine := key + "=" + value
			if l != newLine {
				changed = true
			}
			out = append(out, newLine)
			continue
		}
		out = append(out, l)
	}
	if !found {
		out = append(out, key+"="+value)
		changed = true
	}
	return out, changed
}
