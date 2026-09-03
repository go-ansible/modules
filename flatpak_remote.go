package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleFlatpakRemote implements (a subset of) Ansible's
// `flatpak_remote` module: adds or removes a flatpak remote
// (repository).
//
// Args: name (string, required); flatpakrepo_url (string, required
// when state=present); method (system|user, default "system"); state
// (present|absent, default "present").
//
// Simplifications vs real flatpak_remote: no `enabled`
// (enable/disable-without-removing) or custom `executable` support —
// real flatpak_remote also documents that "existing remotes are not
// updated", which this port preserves (state=present on an
// already-tapped remote is always a no-op, even if flatpakrepo_url
// differs from what was originally used).
func moduleFlatpakRemote(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	method := argString(args, "method", "system")
	if method != "system" && method != "user" {
		return Result{}, errArg("flatpak_remote: method must be system or user, got %q", method)
	}
	state := argString(args, "state", "present")
	methodFlag := "--" + method

	present, err := flatpakRemotePresent(ctx, conn, methodFlag, name)
	if err != nil {
		return Result{}, err
	}

	switch state {
	case "present":
		if present {
			return Ok(name + " already added"), nil
		}
		url, err := requireString(args, "flatpakrepo_url")
		if err != nil {
			return Result{}, errArg("flatpak_remote: flatpakrepo_url is required when state is present")
		}
		if _, err := run(ctx, conn, "flatpak remote-add "+methodFlag+" "+shellQuote(name)+" "+shellQuote(url)); err != nil {
			return Result{}, err
		}
		return Changed(name + " added"), nil

	case "absent":
		if !present {
			return Ok(name + " already absent"), nil
		}
		if _, err := run(ctx, conn, "flatpak remote-delete "+methodFlag+" "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " removed"), nil

	default:
		return Result{}, errArg("flatpak_remote: state must be present or absent, got %q", state)
	}
}

func flatpakRemotePresent(ctx context.Context, conn remoteexec.Connection, methodFlag, name string) (bool, error) {
	res, err := runStatus(ctx, conn, "flatpak remotes "+methodFlag+" --columns=name 2>/dev/null | grep -qxF "+shellQuote(name))
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}
