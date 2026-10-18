package modules

import (
	"sort"
	"strings"
	"testing"
)

// TestCollectionAttributionIsTotal is what keeps the three lists from
// drifting the way the assumption they replace did. A module added
// without an entry fails here rather than quietly printing as
// ansible.builtin, which is how the old mistake survived 514 modules.
func TestCollectionAttributionIsTotal(t *testing.T) {
	seen := map[string]string{}
	for coll, list := range map[string][]string{
		"ansible.builtin":   builtinModules,
		"ansible.posix":     posixModules,
		"community.general": communityGeneralModules,
	} {
		for _, n := range list {
			if other, dup := seen[n]; dup {
				t.Errorf("%q is listed under both %s and %s; the attribution must be unambiguous", n, other, coll)
			}
			seen[n] = coll
		}
	}

	registered := Default().Names()
	for _, n := range registered {
		if seen[n] == "" {
			t.Errorf("%q is registered with no collection; add it to the right list in collections.go "+
				"(measure with `ansible-doc -l <collection> -t module`, do not guess)", n)
		}
	}
	for n := range seen {
		if _, ok := Default().Get(n); !ok {
			t.Errorf("%q has a collection but is not registered; the list has outlived its module", n)
		}
	}

	// The counts as measured on 2026-10-18 against ansible-core 2.21.4.
	// They are allowed to grow, never to shrink silently: a drop means
	// modules left the registry without anyone saying so.
	if len(registered) < 576 {
		t.Errorf("%d modules registered, was 576 on 2026-10-18 — modules have disappeared", len(registered))
	}
	if len(posixModules) != 14 {
		t.Errorf("ansible.posix has %d entries; the real collection has exactly 14 and all are ported", len(posixModules))
	}
}

// Real ansible-doc prints the module's OWN collection. These three are
// the shapes that matter, and the middle one is the case that was wrong
// for every non-core module in the registry.
func TestFQCNNamesTheRealCollection(t *testing.T) {
	for name, want := range map[string]string{
		"copy":                         "ansible.builtin.copy",
		"debug":                        "ansible.builtin.debug",
		"rocketchat":                   "community.general.rocketchat",
		"campfire":                     "community.general.campfire",
		"bigpanda":                     "community.general.bigpanda",
		"synchronize":                  "ansible.posix.synchronize",
		"community.general.rocketchat": "community.general.rocketchat",
	} {
		if got := FQCN(name); got != want {
			t.Errorf("FQCN(%q) = %q, want %q", name, got, want)
		}
	}
	// An unregistered name keeps the old fallback rather than printing a
	// bare word, but it must not claim to be in a real collection it is
	// not in.
	if got := FQCN("no_such_module"); got != "ansible.builtin.no_such_module" {
		t.Errorf("FQCN of an unknown name = %q", got)
	}
	if Collection("no_such_module") != "" {
		t.Error("an unregistered name was given a collection")
	}
}

// A sampled control: the ansible.posix 14 are a closed set small enough
// to name, so getting one of them wrong is a mistake worth catching by
// hand rather than only in aggregate.
func TestPosixModulesAreTheRealFourteen(t *testing.T) {
	want := []string{
		"acl", "at", "authorized_key", "firewalld", "firewalld_info",
		"mount", "patch", "rhel_facts", "rhel_rpm_ostree", "rpm_ostree_upgrade",
		"seboolean", "selinux", "synchronize", "sysctl",
	}
	got := append([]string(nil), posixModules...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("ansible.posix list drifted:\n got  %v\n want %v", got, want)
	}
}
