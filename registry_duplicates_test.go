package modules

import (
	"os"
	"regexp"
	"testing"
)

// TestNoDuplicateRegistrations reads registry.go and fails if a module
// name is registered twice.
//
// Register overwrites, so a second registration of the same name does not
// fail, does not warn, and leaves the FIRST module unreachable -- a whole
// module silently gone, with every test for it still passing because the
// tests call the function directly rather than through the registry.
//
// It is also the premise that lets go-ansible/playbook ACCEPT
// `collections:` as a keyword with no effect: this registry is a single
// flat namespace (NormalizeName strips every known collection prefix),
// so a bare name resolves to exactly one module and a search path has
// nothing to search. If two collections ever contribute the same bare
// name, that stops being true here first, and this is where it shows.
func TestNoDuplicateRegistrations(t *testing.T) {
	src, err := os.ReadFile("registry.go")
	if err != nil {
		t.Fatal(err)
	}
	names := regexp.MustCompile(`r\.Register\("([^"]+)"`).FindAllStringSubmatch(string(src), -1)
	if len(names) < 100 {
		t.Fatalf("read only %d registrations out of registry.go; the check is broken and "+
			"a pass would mean nothing", len(names))
	}

	seen := map[string]bool{}
	for _, m := range names {
		if seen[m[1]] {
			t.Errorf("%q is registered twice: the second overwrites the first, "+
				"leaving a module unreachable", m[1])
		}
		seen[m[1]] = true
	}

	// And the registry really holds one entry per registration -- the
	// count above is of SOURCE LINES, and a map silently absorbing a
	// duplicate is exactly what this is looking for.
	if got := len(Default().Names()); got != len(names) {
		t.Errorf("registry holds %d modules for %d registrations", got, len(names))
	}
}
