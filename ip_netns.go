package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIPNetns implements Ansible's `ip_netns` module
// (community.general): creates or deletes a Linux network namespace,
// via `ip netns add`/`ip netns del`/`ip netns list`.
//
// Args: name (string, required); state (present|absent, default
// "present").
//
// Existence is checked via a plain substring match of `name` against
// `ip netns list`'s output — matching real ip_netns.py's own `self.name
// in out` check exactly, including its documented-by-implication
// caveat that this is NOT an exact-line match: a namespace named
// "foo" would also read as "present" if only a differently-named
// namespace containing "foo" as a substring (e.g. "foobar") exists.
// This port replicates that faithfully rather than "fixing" it, since
// it's simple, harmless in the overwhelmingly common case, and exactly
// what real ip_netns.py itself does.
func moduleIPNetns(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "name", "")
	if name == "" {
		return Result{}, errArg("ip_netns: missing required argument: name")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("ip_netns: state must be present or absent, got %q", state)
	}

	exists, err := ipNetnsExists(ctx, conn, name)
	if err != nil {
		return Result{}, err
	}

	if state == "present" {
		if exists {
			return Ok(name + " already exists"), nil
		}
		if _, err := run(ctx, conn, "ip netns add "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " created"), nil
	}

	if !exists {
		return Ok(name + " already absent"), nil
	}
	if _, err := run(ctx, conn, "ip netns del "+shellQuote(name)); err != nil {
		return Result{}, err
	}
	return Changed(name + " deleted"), nil
}

func ipNetnsExists(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	out, err := run(ctx, conn, "ip netns list")
	if err != nil {
		return false, err
	}
	return strings.Contains(out, name), nil
}
