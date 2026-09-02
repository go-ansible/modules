package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDpkgSelections implements Ansible's `dpkg_selections` module:
// sets a package's dpkg selection state via `dpkg --set-selections`.
//
// Args: name (string, required); selection (string, required — one of
// install, hold, deinstall, purge).
func moduleDpkgSelections(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	selection, err := requireString(args, "selection")
	if err != nil {
		return Result{}, err
	}
	switch selection {
	case "install", "hold", "deinstall", "purge":
	default:
		return Result{}, errArg("dpkg_selections: selection must be install, hold, deinstall, or purge, got %q", selection)
	}

	current, err := run(ctx, conn, "dpkg --get-selections "+shellQuote(name)+" 2>/dev/null || true")
	if err != nil {
		return Result{}, err
	}
	if dpkgSelectionMatches(current, name, selection) {
		return Ok(fmt.Sprintf("%s already set to %s", name, selection)), nil
	}

	cmd := "echo " + shellQuote(name+" "+selection) + " | dpkg --set-selections"
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed(fmt.Sprintf("%s set to %s", name, selection)), nil
}

// dpkgSelectionMatches reports whether `dpkg --get-selections`'s output
// already shows name at selection (its two columns are tab-separated).
func dpkgSelectionMatches(out, name, selection string) bool {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == name {
			return fields[1] == selection
		}
	}
	return false
}
