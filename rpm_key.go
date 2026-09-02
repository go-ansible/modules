package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRpmKey implements (a subset of) Ansible's `rpm_key` module:
// imports or removes a GPG key from the RPM database, the RPM-family
// equivalent of apt_key.go.
//
// Args: key (string, required) — a URL, a path to a key file on the
// target, or (for state=absent only) a key ID/fingerprint identifying
// an already-imported key; state (present|absent, default "present").
//
// Simplifications vs real rpm_key: no `fingerprint` verification (real
// rpm_key can cross-check an imported key's fingerprint against a
// caller-supplied value; this port trusts `rpm --import` outright) and
// no `validate_certs`.
//
// Idempotency for state=present is NOT checked: rpm stores each
// imported key as its own pseudo-package named
// "gpg-pubkey-<8-hex-id>-<8-hex-date>", and telling whether `key`
// (a URL or file path) corresponds to an already-imported package
// requires actually extracting the key's ID first (via `gpg` or `rpm
// -qp --qf`, neither guaranteed present) — out of scope for this
// batch. `rpm --import` of an already-present key is itself a safe
// no-op server-side, so this port always runs it and reports changed,
// the same "can't cheaply tell already-there apart, so always act,
// which is safe but not idempotent-in-reporting" tradeoff apt_repository
// PPA and dnf/apt "latest" already make elsewhere in this package.
//
// state=absent looks up the matching gpg-pubkey-* package by a
// case-insensitive substring match of `key` against `rpm -qa
// 'gpg-pubkey-*'`'s output, then removes every match found — a
// best-effort approach since `key` at this point is expected to be a
// short ID/fingerprint substring of that generated package name, not
// the URL/path form only valid for state=present.
func moduleRpmKey(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	key, err := requireString(args, "key")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")

	switch state {
	case "present":
		if _, err := run(ctx, conn, "rpm --import "+shellQuote(key)); err != nil {
			return Result{}, err
		}
		return Changed("imported key " + key), nil

	case "absent":
		matches, err := rpmKeyPackagesMatching(ctx, conn, key)
		if err != nil {
			return Result{}, err
		}
		if len(matches) == 0 {
			return Ok("no imported key matches " + key), nil
		}
		for _, pkg := range matches {
			if _, err := run(ctx, conn, "rpm -e "+shellQuote(pkg)); err != nil {
				return Result{}, err
			}
		}
		return Changed("removed " + strings.Join(matches, ", ")), nil

	default:
		return Result{}, errArg("rpm_key: state must be present or absent, got %q", state)
	}
}

// rpmKeyPackagesMatching returns every installed gpg-pubkey-* package
// name whose name contains key (case-insensitive).
func rpmKeyPackagesMatching(ctx context.Context, conn remoteexec.Connection, key string) ([]string, error) {
	res, err := runStatus(ctx, conn, "rpm -qa 'gpg-pubkey-*' 2>/dev/null")
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, nil
	}
	needle := strings.ToLower(key)
	var matches []string
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(strings.ToLower(line), needle) {
			matches = append(matches, line)
		}
	}
	return matches, nil
}
