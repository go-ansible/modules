package modules

import (
	"strings"
	"testing"

	pcre "github.com/go-regexp/engine"
)

func mustRe(t *testing.T, p string) *pcre.Regexp {
	t.Helper()
	re, err := pcre.Compile(p)
	if err != nil {
		t.Fatalf("compile %q: %v", p, err)
	}
	return re
}

// Every expectation below was measured against a real ansible-core
// 2.21.4 run on the same three-line file:
//
//	alpha=1
//	beta=2
//	alpha=3
func TestLineinfileAgainstMeasuredReal(t *testing.T) {
	base := []string{"alpha=1", "beta=2", "alpha=3"}

	t.Run("no firstmatch edits the LAST match", func(t *testing.T) {
		// real: alpha=1 | beta=2 | alpha=NEW
		got, out := applyLineinfilePresent(base, lineinfileSpec{
			line: "alpha=NEW", re: mustRe(t, "^alpha="), state: "present"})
		if strings.Join(got, "|") != "alpha=1|beta=2|alpha=NEW" {
			t.Errorf("got %q; real rewrites the LAST match, not the first", strings.Join(got, "|"))
		}
		if out.msg != "line replaced" {
			t.Errorf("msg = %q", out.msg)
		}
	})

	t.Run("firstmatch edits the first", func(t *testing.T) {
		// real: alpha=NEW | beta=2 | alpha=3
		got, _ := applyLineinfilePresent(base, lineinfileSpec{
			line: "alpha=NEW", re: mustRe(t, "^alpha="), firstmatch: true, state: "present"})
		if strings.Join(got, "|") != "alpha=NEW|beta=2|alpha=3" {
			t.Errorf("got %q", strings.Join(got, "|"))
		}
	})

	t.Run("backrefs with no match does nothing at all", func(t *testing.T) {
		// real: file unchanged, msg empty, changed false -- NOT an append
		got, out := applyLineinfilePresent(base, lineinfileSpec{
			line: `zzz=\1`, re: mustRe(t, "^zzz=(.*)"), backrefs: true, state: "present"})
		if strings.Join(got, "|") != "alpha=1|beta=2|alpha=3" {
			t.Errorf("got %q, want the file untouched", strings.Join(got, "|"))
		}
		if out.changed || out.msg != "" {
			t.Errorf("outcome = %+v, want nothing to have happened", out)
		}
	})

	t.Run("backrefs with a match expands the groups", func(t *testing.T) {
		// real: alpha=1 | beta=2 | alpha=[3]
		got, out := applyLineinfilePresent(base, lineinfileSpec{
			line: `alpha=[\1]`, re: mustRe(t, "^alpha=(.*)"), backrefs: true, state: "present"})
		if strings.Join(got, "|") != "alpha=1|beta=2|alpha=[3]" {
			t.Errorf("got %q", strings.Join(got, "|"))
		}
		if out.msg != "line replaced" {
			t.Errorf("msg = %q", out.msg)
		}
	})

	t.Run("search_string is a substring, not a pattern", func(t *testing.T) {
		// real: alpha=1 | beta=NEW | alpha=3
		got, out := applyLineinfilePresent(base, lineinfileSpec{
			line: "beta=NEW", searchString: "beta", state: "present"})
		if strings.Join(got, "|") != "alpha=1|beta=NEW|alpha=3" {
			t.Errorf("got %q", strings.Join(got, "|"))
		}
		if out.msg != "line replaced" {
			t.Errorf("msg = %q", out.msg)
		}
		// and a regex metacharacter is NOT one: "a.pha" matches nothing
		got, out = applyLineinfilePresent(base, lineinfileSpec{
			line: "x", searchString: "a.pha", state: "present"})
		if out.msg != "line added" || got[len(got)-1] != "x" {
			t.Errorf("a dot was treated as a wildcard: %q %+v", strings.Join(got, "|"), out)
		}
	})
}

func TestLineinfileInsertPositions(t *testing.T) {
	base := []string{"a", "b", "c"}
	for _, tc := range []struct {
		name string
		spec lineinfileSpec
		want string
	}{
		{"insertafter a", lineinfileSpec{line: "X", insertAfter: "^a$", state: "present"}, "a|X|b|c"},
		{"insertbefore c", lineinfileSpec{line: "X", insertBefore: "^c$", state: "present"}, "a|b|X|c"},
		{"insertafter EOF", lineinfileSpec{line: "X", insertAfter: "EOF", state: "present"}, "a|b|c|X"},
		{"insertbefore BOF", lineinfileSpec{line: "X", insertBefore: "BOF", state: "present"}, "X|a|b|c"},
		{"insertafter that matches nothing appends", lineinfileSpec{line: "X", insertAfter: "^zz$", state: "present"}, "a|b|c|X"},
		{"no insert anchor at all appends", lineinfileSpec{line: "X", state: "present"}, "a|b|c|X"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.spec
			if s.insertAfter != "" && s.insertAfter != "EOF" && s.insertAfter != "BOF" {
				s.insAfterRe = mustRe(t, s.insertAfter)
			}
			if s.insertBefore != "" && s.insertBefore != "BOF" {
				s.insBeforeRe = mustRe(t, s.insertBefore)
			}
			got, out := applyLineinfilePresent(base, s)
			if strings.Join(got, "|") != tc.want {
				t.Errorf("got %q, want %q", strings.Join(got, "|"), tc.want)
			}
			if !out.changed {
				t.Error("nothing changed")
			}
		})
	}
}

// A line already present must not be inserted again, which is what
// makes a second run report ok instead of changed.
func TestLineinfileIdempotent(t *testing.T) {
	base := []string{"a", "X", "b"}
	got, out := applyLineinfilePresent(base, lineinfileSpec{
		line: "X", insertAfter: "^a$", insAfterRe: mustRe(t, "^a$"), state: "present"})
	if out.changed {
		t.Errorf("a line already in the file was inserted again: %q", strings.Join(got, "|"))
	}
}

func TestLineinfileAbsentMatchers(t *testing.T) {
	base := []string{"alpha=1", "beta=2", "alpha=3"}
	for _, tc := range []struct {
		name string
		spec lineinfileSpec
		want string
		n    int
	}{
		{"regexp removes every match", lineinfileSpec{re: mustRe(t, "^alpha="), state: "absent"}, "beta=2", 2},
		{"search_string is a substring", lineinfileSpec{searchString: "eta", state: "absent"}, "alpha=1|alpha=3", 1},
		{"no pattern means an exact line", lineinfileSpec{line: "beta=2", state: "absent"}, "alpha=1|alpha=3", 1},
		{"nothing matches", lineinfileSpec{line: "zz", state: "absent"}, "alpha=1|beta=2|alpha=3", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, out := applyLineinfileAbsent(base, tc.spec)
			if strings.Join(got, "|") != tc.want {
				t.Errorf("got %q, want %q", strings.Join(got, "|"), tc.want)
			}
			if out.removed != tc.n {
				t.Errorf("removed = %d, want %d", out.removed, tc.n)
			}
			if tc.n > 0 && out.msg != strings.ReplaceAll("N line(s) removed", "N", itoa(tc.n)) {
				t.Errorf("msg = %q", out.msg)
			}
		})
	}
}

func itoa(n int) string { return string(rune('0' + n)) }

func TestExpandBackrefs(t *testing.T) {
	re := mustRe(t, `^(\w+)=(\d+)$`)
	g := re.FindStringSubmatch("alpha=3")
	if g == nil {
		t.Fatal("no match")
	}
	for _, tc := range []struct{ tmpl, want string }{
		{`\1`, "alpha"},
		{`\2`, "3"},
		{`\1=\2`, "alpha=3"},
		{`[\1]`, "[alpha]"},
		{`\g<1>`, "alpha"},
		{`\\1`, `\1`}, // an escaped backslash is literal
		{`\9`, ""},    // a group that does not exist expands to nothing
		{`no refs`, "no refs"},
	} {
		if got := expandBackrefs(re, g, tc.tmpl); got != tc.want {
			t.Errorf("expand(%q) = %q, want %q", tc.tmpl, got, tc.want)
		}
	}
}
