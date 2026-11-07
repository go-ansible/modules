package modules

import (
	"context"
	"strings"
	"testing"
)

// The defect these pin is #110: a task written against a documented alias
// failed before doing anything. The tests go through Default().Run rather
// than calling a module directly, because the fix is the DISPATCH path and a
// test that called the module would pass with no fix at all.

func TestRunResolvesFileDestAlias(t *testing.T) {
	// The task from the issue, reduced: `dest` with no `path`.
	conn := &curlConn{}
	res, err := Default().Run(context.Background(), "file", conn, map[string]any{
		"dest": "/tmp/does-not-matter", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed && strings.Contains(res.Msg, "path") {
		t.Fatalf("still asking for path: %q", res.Msg)
	}
}

func TestRunResolvesEveryReadAlias(t *testing.T) {
	for _, tc := range []struct {
		module, alias, canonical string
	}{
		{"file", "dest", "path"},
		{"file", "name", "path"},
		{"stat", "dest", "path"},
		{"stat", "name", "path"},
		{"lineinfile", "dest", "path"},
		{"lineinfile", "destfile", "path"},
		{"lineinfile", "name", "path"},
		{"lineinfile", "regex", "regexp"},
		{"lineinfile", "value", "line"},
		{"blockinfile", "destfile", "path"},
		{"blockinfile", "content", "block"},
		{"replace", "dest", "path"},
		{"systemd", "service", "name"},
		{"systemd_service", "unit", "name"},
		{"stat", "mime-type", "get_mime"},
		{"stat", "checksum_algo", "checksum_algorithm"},
	} {
		got := resolveAliases(tc.module, map[string]any{tc.alias: "v"})
		if got[tc.canonical] != "v" {
			t.Errorf("%s: %s did not reach %s (got %#v)", tc.module, tc.alias, tc.canonical, got[tc.canonical])
		}
		// Real ADDS the canonical key and leaves the alias in place.
		if got[tc.alias] != "v" {
			t.Errorf("%s: %s was consumed; real leaves it alongside", tc.module, tc.alias)
		}
	}
}

// Real's assignment is unconditional (parameters.py:245), so an alias
// overwrites an explicitly given canonical. This is the case a reader is
// most likely to assume backwards, which is why it is pinned rather than
// left to the table.
func TestAnAliasWinsOverTheCanonicalName(t *testing.T) {
	got := resolveAliases("file", map[string]any{"path": "/from-path", "dest": "/from-dest"})
	if got["path"] != "/from-dest" {
		t.Errorf("path = %v, want /from-dest: real assigns parameters[k] = parameters[alias] unconditionally", got["path"])
	}
}

// Aliases are applied in declaration order, so the LAST present one wins.
func TestTheLastAliasInRealsOrderWins(t *testing.T) {
	// lineinfile declares ['dest', 'destfile', 'name'].
	got := resolveAliases("lineinfile", map[string]any{"dest": "/a", "name": "/c"})
	if got["path"] != "/c" {
		t.Errorf("path = %v, want /c (name is last of dest, destfile, name)", got["path"])
	}
	got = resolveAliases("lineinfile", map[string]any{"dest": "/a", "destfile": "/b"})
	if got["path"] != "/b" {
		t.Errorf("path = %v, want /b (destfile is after dest)", got["path"])
	}
}

// The engine reuses an args map across a loop's iterations and across
// retries. Mutating it here would change a later iteration's input.
func TestResolveAliasesDoesNotMutateItsInput(t *testing.T) {
	in := map[string]any{"dest": "/x"}
	out := resolveAliases("file", in)
	if _, leaked := in["path"]; leaked {
		t.Error("the caller's map was modified")
	}
	if out["path"] != "/x" {
		t.Errorf("path = %v, want the resolved value in the COPY", out["path"])
	}
	if len(in) != 1 {
		t.Errorf("input grew to %d keys", len(in))
	}
}

// A module with no alias table, or a task that used none, pays nothing and
// gets the same map back — the identity is the cheap path, and asserting it
// keeps a future "always copy" from slipping in unnoticed.
func TestResolveAliasesReturnsTheSameMapWhenThereIsNothingToDo(t *testing.T) {
	args := map[string]any{"path": "/x"}
	if got := resolveAliases("file", args); len(got) != len(args) || got["path"] != "/x" {
		t.Errorf("got %#v, want the input unchanged", got)
	}
	unknown := map[string]any{"whatever": 1}
	if got := resolveAliases("no_such_module", unknown); len(got) != 1 {
		t.Errorf("got %#v for a module with no table", got)
	}
}

// A collection-qualified name must find the table, since that is how a
// playbook most often writes it.
func TestResolveAliasesAcceptsAnFQCN(t *testing.T) {
	got := resolveAliases("ansible.builtin.file", map[string]any{"dest": "/x"})
	if got["path"] != "/x" {
		t.Errorf("path = %v; NormalizeName should have found the table", got["path"])
	}
}

// Every alias in the table must name a module that exists, or the entry is
// a note about a module nobody can run.
func TestEveryAliasTableEntryNamesARegisteredModule(t *testing.T) {
	r := Default()
	for name := range moduleArgAliases {
		if _, ok := r.Get(name); !ok {
			t.Errorf("moduleArgAliases has %q, which is not registered", name)
		}
	}
}
