package modules

import (
	"fmt"
	"sort"
	"strings"
)

// EnvironmentKey is the wire flag carrying a task's merged
// `environment:` into the module, alongside CheckModeKey and
// DiffModeKey. It travels in args for the same reason those do: one
// run-wide concern should not add a parameter to 566 signatures.
//
// The engine has already merged the play's entries with the task's own
// (task wins) and templated every value, so what arrives here is final.
const EnvironmentKey = "_ansible_environment"

// EnvironmentPrefix renders a task's environment: as the shell
// `export` that must precede its command, or "" when there is none.
//
// An export rather than a `FOO=bar cmd` prefix, because a command may
// itself be `cd somewhere && real-command`: assignments written in
// front would apply to the `cd` alone, and the command that matters
// would run without them.
//
// Keys are sorted so the same task produces the same command line
// twice — a map's order is not one.
func EnvironmentPrefix(args map[string]any) string {
	env, ok := args[EnvironmentKey].(map[string]any)
	if !ok || len(env) == 0 {
		return ""
	}
	names := make([]string, 0, len(env))
	for k := range env {
		names = append(names, k)
	}
	sort.Strings(names)

	assignments := make([]string, 0, len(names))
	for _, name := range names {
		assignments = append(assignments, name+"="+shellQuote(environmentValue(env[name])))
	}
	return "export " + strings.Join(assignments, " ") + "; "
}

// environmentValue renders a YAML value as real Ansible puts it in the
// environment, which is Python's str(): a bool is "True" or "False",
// capitalised, NOT Go's "true". Measured — a script testing
// `[ "$FLAG" = "True" ]` would otherwise never match.
func environmentValue(v any) string {
	switch t := v.(type) {
	case bool:
		if t {
			return "True"
		}
		return "False"
	case nil:
		return "None"
	case string:
		return t
	}
	return fmt.Sprintf("%v", v)
}
