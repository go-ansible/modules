package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGetent implements (a subset of) Ansible's `getent` module: runs
// `getent <database> [<key>]` on the target and parses the output into
// a map keyed by each entry's first field (the entry's own name),
// valued by its remaining fields.
//
// Real ansible.builtin.getent nests its result under
// ansible_facts.getent_<database>. This port, following how set_fact
// and stat already shape their results, puts it under
// Extra["getent_<database>"] instead of Facts — a deliberate deviation
// from real Ansible's exact contract, documented here.
//
// Args: database (string, required); key (string, optional); split
// (string, optional — the field separator; defaults to ":" for every
// database except "hosts", which getent prints whitespace-separated,
// not colon-separated); fail_key (bool, default true — fail when key is
// given but not found).
func moduleGetent(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	database, err := requireString(args, "database")
	if err != nil {
		return Result{}, err
	}
	key := argString(args, "key", "")
	split := argString(args, "split", "")
	if split == "" {
		if database == "hosts" {
			split = " "
		} else {
			split = ":"
		}
	}
	failKey := argBool(args, "fail_key", true)
	extraKey := "getent_" + database

	res, err := runStatus(ctx, conn, getentCmd(database, key))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		if key != "" && failKey {
			return Fail(fmt.Sprintf("unable to find key %q in getent database %q", key, database)), nil
		}
		return Ok("").WithExtra(extraKey, map[string]any{}), nil
	}

	entries := parseGetent(res.Stdout, split)
	return Ok("").WithExtra(extraKey, entries), nil
}

// getentCmd builds the getent invocation for moduleGetent, separated
// out so its exact shape can be asserted directly in tests.
func getentCmd(database, key string) string {
	cmd := "getent " + shellQuote(database)
	if key != "" {
		cmd += " " + shellQuote(key)
	}
	return cmd
}

// parseGetent splits getent's colon- (or, for "hosts", whitespace-)
// delimited output into a map from each entry's first field to its
// remaining fields.
func parseGetent(out, split string) map[string]any {
	entries := map[string]any{}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var fields []string
		if split == " " {
			fields = strings.Fields(line)
		} else {
			fields = strings.Split(line, split)
		}
		if len(fields) == 0 || fields[0] == "" {
			continue
		}
		entries[fields[0]] = fields[1:]
	}
	return entries
}
