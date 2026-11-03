package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// targetEnv reads one environment variable ON THE TARGET, returning ""
// when it is unset.
//
// This is what real Ansible's `env_fallback` resolves to. The difference
// worth stating is WHERE it looks: real evaluates env_fallback inside
// the module process, and that process runs on the target, so the
// variable it finds is the target's — including anything a play's
// `environment:` put there. A module in this package runs on the control
// node and reaches the host through a connection, so reading
// os.Getenv here would silently substitute the OPERATOR's environment
// for the target's. For a variable whose whole purpose is to carry a
// credential — BITBUCKET_CLIENT_SECRET, say — that is not a cosmetic
// difference: it would send the operator's own token to a host that was
// meant to supply its own.
//
// The name is validated rather than quoted: a shell-quoted name would be
// safe to run, but an environment variable name that is not a name is a
// caller's mistake, and failing on it beats running a command that
// quietly finds nothing. An unset variable is "" with a nil error,
// because `printenv` exits non-zero for one and that is not a failure.
//
// The value never reaches argv: it is `printenv`'s stdout.
func targetEnv(ctx context.Context, conn remoteexec.Connection, name string) (string, error) {
	if !validEnvName(name) {
		return "", fmt.Errorf("targetEnv: %q is not an environment variable name", name)
	}
	res, err := conn.Exec(ctx, "printenv "+name, nil)
	if err != nil {
		return "", fmt.Errorf("reading %s from the target's environment: %w", name, err)
	}
	if res.RC != 0 {
		return "", nil
	}
	// printenv adds exactly one newline. Only that one is removed: a
	// value with its own trailing whitespace keeps it, and a value that
	// is genuinely empty stays empty.
	v := strings.TrimSuffix(res.Stdout, "\n")
	return strings.TrimSuffix(v, "\r"), nil
}

// validEnvName reports whether name is a POSIX environment variable
// name: a letter or underscore, then letters, digits and underscores.
func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_':
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
