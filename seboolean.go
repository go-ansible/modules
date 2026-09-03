package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSeboolean implements Ansible's `seboolean` module: toggles an
// SELinux boolean via `setsebool`, checking its current value first via
// `getsebool` for idempotency.
//
// Args: name (string, required); state (bool, required) — the desired
// boolean value; persistent (bool, default false) — adds `-P` so the
// change survives a reboot.
//
// Simplifications vs real seboolean: no `ignore_selinux_state` (real
// seboolean uses it to skip its own SELinux-enabled check in a chrooted
// environment where the check would be unreliable; this port has no
// such check to skip in the first place — it always just runs
// getsebool/setsebool and lets a non-SELinux target's command failure
// surface as a normal Go error, the same "fail loud on a missing
// command" behavior selinux.go documents for its own getenforce probe).
func moduleSeboolean(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	if _, ok := args["state"]; !ok {
		return Result{}, errArg("seboolean: missing required argument: state")
	}
	want := argBool(args, "state", false)
	persistent := argBool(args, "persistent", false)

	cur, err := getseboolCurrent(ctx, conn, name)
	if err != nil {
		return Result{}, err
	}
	if cur == want {
		return Ok(name + " already " + seboolWord(want)), nil
	}

	cmd := "setsebool"
	if persistent {
		cmd += " -P"
	}
	cmd += " " + shellQuote(name) + " " + seboolWord(want)
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed(name + " set to " + seboolWord(want)), nil
}

func seboolWord(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

// getseboolCurrent parses `getsebool <name>`'s "name --> on/off" output.
func getseboolCurrent(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	out, err := run(ctx, conn, "getsebool "+shellQuote(name))
	if err != nil {
		return false, err
	}
	_, value, ok := strings.Cut(out, "--> ")
	if !ok {
		return false, errArg("seboolean: unexpected getsebool output for %q: %q", name, out)
	}
	return strings.TrimSpace(value) == "on", nil
}
