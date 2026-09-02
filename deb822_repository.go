package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDeb822Repository implements (a subset of) Ansible's
// `deb822_repository` module: writes (or removes) a structured
// RFC822/deb822-format APT source file under
// /etc/apt/sources.list.d/<name>.sources — the modern replacement for
// the one-line `deb ...` sources apt_repository.go's plain-line path
// composes.
//
// Args: name (string, required) — used only as the destination
// filename stem, matching how real deb822_repository derives its
// default filename from `name` when `filename` isn't given (this port
// has no separate `filename` argument at all: filename is always
// derived from name); types ([]string, default ["deb"]) — "deb" or
// "deb-src"; uris ([]string, required); suites ([]string, required);
// components ([]string, optional); signed_by (string, optional) — a
// URL, path, fingerprint, or inline key block, written verbatim into
// the stanza's Signed-By field; state (present|absent, default
// "present").
//
// Simplifications vs real deb822_repository: none of allow_insecure,
// allow_weak, architectures, by_hash, check_date, check_valid_until,
// date_max_future, enabled, trusted, or the dozen other apt-preferences
// knobs real deb822_repository exposes are supported — this port
// writes the handful of fields that make a source resolvable
// (Types/URIs/Suites/Components/Signed-By) and nothing else. Real
// deb822_repository also validates and normalizes signed_by (fetching
// a URL into a keyring file under /etc/apt/keyrings/ when it looks like
// one); this port writes whatever string it's given straight into the
// Signed-By field, which is only valid deb822 syntax when signed_by is
// itself a path, a fingerprint, or an inline ASCII-armored block —
// passing a bare URL here does NOT fetch and materialize a keyring
// file the way real deb822_repository does.
//
// Idempotency: like apt_repository.go's plain-line path, present is
// checked by comparing the destination file's existing content against
// the wanted stanza byte-for-byte.
func moduleDeb822Repository(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("deb822_repository: state must be present or absent, got %q", state)
	}
	path := "/etc/apt/sources.list.d/" + name + ".sources"

	if state == "absent" {
		exists, err := pathExists(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Ok(path + " already absent"), nil
		}
		if _, err := run(ctx, conn, "rm -f "+shellQuote(path)); err != nil {
			return Result{}, err
		}
		return Changed(path + " removed"), nil
	}

	uris := argStringList(args, "uris")
	if len(uris) == 0 {
		return Result{}, errArg("deb822_repository: uris is required when state is present")
	}
	suites := argStringList(args, "suites")
	if len(suites) == 0 {
		return Result{}, errArg("deb822_repository: suites is required when state is present")
	}
	types := argStringList(args, "types")
	if len(types) == 0 {
		types = []string{"deb"}
	}
	components := argStringList(args, "components")
	signedBy := argString(args, "signed_by", "")

	stanza := deb822Stanza(types, uris, suites, components, signedBy)

	exists, err := pathExists(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}
	if exists {
		current, err := run(ctx, conn, "cat "+shellQuote(path))
		if err != nil {
			return Result{}, err
		}
		if current == strings.TrimRight(stanza, "\n") {
			return Ok(path + " unchanged"), nil
		}
	}

	cmd := "mkdir -p /etc/apt/sources.list.d && printf '%s' " + shellQuote(stanza) + " > " + shellQuote(path)
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed(path), nil
}

// deb822Stanza composes a single deb822 source stanza, separated out so
// its exact shape can be asserted directly in tests.
func deb822Stanza(types, uris, suites, components []string, signedBy string) string {
	var b strings.Builder
	b.WriteString("Types: " + strings.Join(types, " ") + "\n")
	b.WriteString("URIs: " + strings.Join(uris, " ") + "\n")
	b.WriteString("Suites: " + strings.Join(suites, " ") + "\n")
	if len(components) > 0 {
		b.WriteString("Components: " + strings.Join(components, " ") + "\n")
	}
	if signedBy != "" {
		b.WriteString("Signed-By: " + signedBy + "\n")
	}
	return b.String()
}
