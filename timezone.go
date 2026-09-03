package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleTimezone implements (a subset of) Ansible's `timezone`
// (community.general) module for Linux targets: sets the system clock's
// timezone and/or whether the hardware clock is kept in UTC or local
// time.
//
// Args: name (string) — a zoneinfo name like "Asia/Tokyo"; hwclock
// (string, "local" or "UTC", aliased from `rtc` in real timezone — this
// port only accepts the canonical `hwclock`, per this package's
// standing convention, see known_hosts.go's doc comment). At least one
// of name/hwclock is required.
//
// On a target where `timedatectl` is available, both name and hwclock
// are read and set through it (`timedatectl show -p Timezone --value`,
// `set-timezone`; `timedatectl show -p LocalRTC --value`, `set-local-
// rtc`). Otherwise (`command -v timedatectl` fails), `name` falls back
// to the Debian-style mechanism real timezone also uses on non-systemd
// Linux: reading/writing `/etc/timezone` and re-pointing the
// `/etc/localtime` symlink at `/usr/share/zoneinfo/<name>`.
//
// Simplifications vs real timezone: Linux only — real timezone also
// supports SmartOS (`sm-set-timezone`), macOS (`systemsetup`), BSD
// (`/etc/localtime` only, no systemd/Debian split), and AIX (`chtz`);
// this port does not attempt OS detection for those and simply runs the
// Linux-shaped commands above regardless of target OS, so it not being
// Linux surfaces as a command-not-found style failure rather than a
// clean, named unsupported-OS error — a real gap, but consistent with
// this port's general Linux-first scope elsewhere in this batch
// (modprobe.go, kernel_blacklist.go). `hwclock` without `timedatectl`
// available is NOT implemented: real timezone falls back to editing
// `/etc/sysconfig/clock` or the first line of `/etc/adjtime`, whose
// exact format varies by distribution and hwclock version; guessing at
// that file's layout risked silently corrupting it, so this port fails
// cleanly instead (see synchronize.go's fail-stub convention) rather
// than attempting an under-verified rewrite.
func moduleTimezone(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "name", "")
	hwclock := argString(args, "hwclock", "")
	if hwclock != "" && hwclock != "local" && hwclock != "UTC" {
		return Result{}, errArg("timezone: hwclock must be local or UTC, got %q", hwclock)
	}
	if name == "" && hwclock == "" {
		return Result{}, errArg("timezone: at least one of name or hwclock is required")
	}

	hasTimedatectl, err := timezoneHasTimedatectl(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	changed := false
	var parts []string

	if name != "" {
		if hasTimedatectl {
			cur, err := run(ctx, conn, "timedatectl show -p Timezone --value")
			if err != nil {
				return Result{}, err
			}
			if cur != name {
				if _, err := run(ctx, conn, "timedatectl set-timezone "+shellQuote(name)); err != nil {
					return Result{}, err
				}
				changed = true
			}
		} else {
			cur, err := timezoneCurrentDebian(ctx, conn)
			if err != nil {
				return Result{}, err
			}
			if cur != name {
				if err := writeRemote(ctx, conn, "/etc/timezone", []byte(name+"\n")); err != nil {
					return Result{}, err
				}
				if _, err := run(ctx, conn, "ln -sf "+shellQuote("/usr/share/zoneinfo/"+name)+" /etc/localtime"); err != nil {
					return Result{}, err
				}
				changed = true
			}
		}
		parts = append(parts, "name="+name)
	}

	if hwclock != "" {
		if !hasTimedatectl {
			return Fail("timezone: hwclock is not implemented without timedatectl on this target (see moduleTimezone's doc comment)"), nil
		}
		cur, err := run(ctx, conn, "timedatectl show -p LocalRTC --value")
		if err != nil {
			return Result{}, err
		}
		wantLocal := hwclock == "local"
		curLocal := cur == "yes"
		if wantLocal != curLocal {
			setVal := "false"
			if wantLocal {
				setVal = "true"
			}
			if _, err := run(ctx, conn, "timedatectl set-local-rtc "+setVal); err != nil {
				return Result{}, err
			}
			changed = true
		}
		parts = append(parts, "hwclock="+hwclock)
	}

	msg := strings.Join(parts, " ")
	if changed {
		return Changed(msg), nil
	}
	return Ok(msg + " unchanged"), nil
}

func timezoneHasTimedatectl(ctx context.Context, conn remoteexec.Connection) (bool, error) {
	res, err := runStatus(ctx, conn, "command -v timedatectl >/dev/null 2>&1")
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

// timezoneCurrentDebian reads the current timezone via /etc/timezone,
// falling back to resolving the /etc/localtime symlink's target if that
// file doesn't exist or is empty.
func timezoneCurrentDebian(ctx context.Context, conn remoteexec.Connection) (string, error) {
	res, err := runStatus(ctx, conn, "cat /etc/timezone 2>/dev/null")
	if err != nil {
		return "", err
	}
	if res.RC == 0 {
		if cur := strings.TrimSpace(res.Stdout); cur != "" {
			return cur, nil
		}
	}
	res, err = runStatus(ctx, conn, "readlink -f /etc/localtime 2>/dev/null")
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", nil
	}
	return strings.TrimPrefix(strings.TrimSpace(res.Stdout), "/usr/share/zoneinfo/"), nil
}
