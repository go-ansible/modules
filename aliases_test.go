package modules

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// runModule goes through Registry.Run on purpose: that is where aliases
// are resolved, and calling moduleFile & co. directly would bypass it.
func runModule(t *testing.T, name string, conn remoteexec.Connection, args map[string]any) Result {
	t.Helper()
	res, err := Default().Run(context.Background(), name, conn, args)
	if err != nil {
		t.Fatalf("%s %v: %v", name, args, err)
	}
	if res.Failed {
		t.Fatalf("%s %v: failed: %s", name, args, res.Msg)
	}
	return res
}

func TestResolveAliases(t *testing.T) {
	cases := []struct {
		name     string
		module   string
		in       map[string]any
		want     map[string]any
		warnings []string
	}{
		{
			name:   "alias copied onto canonical, alias key kept",
			module: "file",
			in:     map[string]any{"dest": "/a"},
			want:   map[string]any{"dest": "/a", "path": "/a"},
		},
		{
			name:   "FQCN resolves to the same table entry",
			module: "ansible.builtin.file",
			in:     map[string]any{"name": "/a"},
			want:   map[string]any{"name": "/a", "path": "/a"},
		},
		{
			name:   "canonical alone is untouched",
			module: "file",
			in:     map[string]any{"path": "/a"},
			want:   map[string]any{"path": "/a"},
		},
		{
			// _handle_aliases: parameters[k] = parameters[alias] runs
			// after the warning is recorded, so the alias wins.
			name:     "canonical and alias both given: alias wins, one warning",
			module:   "file",
			in:       map[string]any{"path": "/a", "dest": "/b"},
			want:     map[string]any{"path": "/b", "dest": "/b"},
			warnings: []string{"Both option path and its alias dest are set."},
		},
		{
			// Real warns whether or not the values differ.
			name:     "same value under both names still warns",
			module:   "stat",
			in:       map[string]any{"path": "/a", "name": "/a"},
			want:     map[string]any{"path": "/a", "name": "/a"},
			warnings: []string{"Both option path and its alias name are set."},
		},
		{
			// dest sets path first; name then finds path already set.
			name:     "two aliases, no canonical: last in spec order wins",
			module:   "file",
			in:       map[string]any{"name": "/n", "dest": "/d"},
			want:     map[string]any{"name": "/n", "dest": "/d", "path": "/n"},
			warnings: []string{"Both option path and its alias name are set."},
		},
		{
			name:   "several options of one module",
			module: "lineinfile",
			in:     map[string]any{"destfile": "/f", "regex": "^a", "value": "a=1"},
			want: map[string]any{
				"destfile": "/f", "path": "/f",
				"regex": "^a", "regexp": "^a",
				"value": "a=1", "line": "a=1",
			},
		},
		{
			name:   "module without a table entry is untouched",
			module: "copy",
			in:     map[string]any{"dest": "/a"},
			want:   map[string]any{"dest": "/a"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := make(map[string]any, len(tc.in))
			for k, v := range tc.in {
				before[k] = v
			}
			got, warnings := resolveAliases(tc.module, tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("args = %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(warnings, tc.warnings) {
				t.Errorf("warnings = %q, want %q", warnings, tc.warnings)
			}
			if !reflect.DeepEqual(tc.in, before) {
				t.Errorf("caller's map was modified: %v, was %v", tc.in, before)
			}
		})
	}
}

// The exact task from go-ansible/modules#110.
func TestFileLinkWithDest(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "glauth-2.5.4")
	if err := os.WriteFile(target, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "glauth")
	res := runModule(t, "ansible.builtin.file", local(), map[string]any{
		"src": target, "dest": link, "state": "link",
	})
	if !res.Changed {
		t.Error("want changed")
	}
	got, err := os.Readlink(link)
	if err != nil || got != target {
		t.Fatalf("readlink = %q, %v; want %q", got, err, target)
	}
}

func TestFileAcceptsPathAliases(t *testing.T) {
	for _, key := range []string{"path", "dest", "name"} {
		t.Run(key, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "made")
			res := runModule(t, "file", local(), map[string]any{key: dir, "state": "directory"})
			if !res.Changed {
				t.Error("want changed")
			}
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				t.Fatalf("directory not created: %v", err)
			}
			if _, warned := res.Extra["warnings"]; warned {
				t.Errorf("unexpected warnings: %v", res.Extra["warnings"])
			}
		})
	}
}

func TestFileCanonicalAndAliasBothGiven(t *testing.T) {
	base := t.TempDir()
	viaPath := filepath.Join(base, "via-path")
	viaDest := filepath.Join(base, "via-dest")
	res := runModule(t, "file", local(), map[string]any{
		"path": viaPath, "dest": viaDest, "state": "directory",
	})
	if _, err := os.Stat(viaDest); err != nil {
		t.Errorf("the alias's value should win, as in real: %v", err)
	}
	if _, err := os.Stat(viaPath); err == nil {
		t.Error("the canonical name's value was used; real uses the alias's")
	}
	want := []any{"Both option path and its alias dest are set."}
	if !reflect.DeepEqual(res.Extra["warnings"], want) {
		t.Errorf("warnings = %#v, want %#v", res.Extra["warnings"], want)
	}
}

func TestStatAcceptsPathAliases(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"path", "dest", "name"} {
		t.Run(key, func(t *testing.T) {
			res := runModule(t, "stat", local(), map[string]any{key: p})
			st, _ := res.Extra["stat"].(map[string]any)
			if st["exists"] != true {
				t.Fatalf("stat = %v", res.Extra["stat"])
			}
		})
	}
}

func TestLineinfileAcceptsAliases(t *testing.T) {
	for _, key := range []string{"path", "dest", "destfile", "name"} {
		t.Run(key, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "conf")
			if err := os.WriteFile(p, []byte("a=1\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			res := runModule(t, "lineinfile", local(), map[string]any{
				key: p, "regex": "^a=", "value": "a=2",
			})
			if !res.Changed {
				t.Error("want changed")
			}
			data, err := os.ReadFile(p)
			if err != nil || string(data) != "a=2\n" {
				t.Fatalf("content = %q, %v", data, err)
			}
		})
	}
}

// regex is regexp's alias, so it must trip real's regexp|search_string
// mutual exclusion just as regexp does.
func TestLineinfileRegexAliasIsMutuallyExclusive(t *testing.T) {
	p := filepath.Join(t.TempDir(), "conf")
	if err := os.WriteFile(p, []byte("a=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Default().Run(context.Background(), "lineinfile", local(), map[string]any{
		"path": p, "regex": "^a=", "search_string": "a=", "line": "a=2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || !strings.Contains(res.Msg, "regexp|search_string") {
		t.Fatalf("res = %+v", res)
	}
}

func TestBlockinfileAcceptsAliases(t *testing.T) {
	for _, key := range []string{"path", "dest", "destfile", "name"} {
		t.Run(key, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "conf")
			res := runModule(t, "blockinfile", local(), map[string]any{
				key: p, "content": "hello", "create": true,
			})
			if !res.Changed {
				t.Error("want changed")
			}
			data, err := os.ReadFile(p)
			if err != nil || !strings.Contains(string(data), "\nhello\n") {
				t.Fatalf("content = %q, %v", data, err)
			}
		})
	}
}

func TestReplaceAcceptsAliases(t *testing.T) {
	for _, key := range []string{"path", "dest", "destfile", "name"} {
		t.Run(key, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "conf")
			if err := os.WriteFile(p, []byte("old\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			res := runModule(t, "replace", local(), map[string]any{
				key: p, "regexp": "old", "replace": "new",
			})
			if !res.Changed {
				t.Error("want changed")
			}
			data, err := os.ReadFile(p)
			if err != nil || string(data) != "new\n" {
				t.Fatalf("content = %q, %v", data, err)
			}
		})
	}
}

func TestKnownHostsAcceptsHostAlias(t *testing.T) {
	path := "~/.ssh/known_hosts"
	key := "example.com ssh-rsa AAAA..."
	conn := newFakeConn(map[string]remoteexec.Result{
		"grep -qxF " + shellQuote(key) + " " + shellQuote(path) + " 2>/dev/null": {RC: 0},
	})
	res := runModule(t, "known_hosts", conn, map[string]any{"host": "example.com", "key": key})
	if res.Extra["name"] != "example.com" {
		t.Fatalf("name = %v", res.Extra["name"])
	}
}

func TestAclAcceptsAliases(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getfacl /etc/foo.d":                     {RC: 0, Stdout: ""},
		"setfacl -R -m d:user:joe:rw /etc/foo.d": {RC: 0},
	})
	res := runModule(t, "ansible.posix.acl", conn, map[string]any{
		"name": "/etc/foo.d", "state": "present", "etype": "user", "entity": "joe",
		"permissions": "rw", "default": true, "recurse": true,
	})
	if !res.Changed {
		t.Fatalf("want changed; commands = %q", conn.Commands)
	}
	if want := "setfacl -R -m d:user:joe:rw /etc/foo.d"; !containsString(conn.Commands, want) {
		t.Fatalf("commands = %q, want %q among them", conn.Commands, want)
	}
}

func TestBzrAcceptsParentAlias(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /srv/checkout/.bzr/branch/branch.conf": {RC: 1},
		"cd /srv/checkout && bzr revno":                 {RC: 0, Stdout: "5\n"},
	})
	runModule(t, "bzr", conn, map[string]any{
		"parent": "bzr+ssh://example.org/branch", "dest": "/srv/checkout",
	})
	if want := "bzr branch bzr+ssh://example.org/branch /srv/checkout"; !containsString(conn.Commands, want) {
		t.Fatalf("commands = %q, want %q among them", conn.Commands, want)
	}
}

func TestSubversionAcceptsAliases(t *testing.T) {
	for _, key := range []string{"repo", "name", "repository"} {
		for _, revKey := range []string{"revision", "rev", "version"} {
			t.Run(key+"/"+revKey, func(t *testing.T) {
				checkout := "svn checkout --quiet -r " + shellQuote("42") + " " +
					shellQuote("svn://x/repo") + " " + shellQuote("/d")
				conn := newFakeConn(map[string]remoteexec.Result{
					"test -e " + shellQuote("/d/.svn"): {RC: 1},
				})
				runModule(t, "subversion", conn, map[string]any{
					key: "svn://x/repo", "dest": "/d", revKey: "42",
				})
				if !containsString(conn.Commands, checkout) {
					t.Fatalf("commands = %q, want %q among them", conn.Commands, checkout)
				}
			})
		}
	}
}

func TestLogrotateAcceptsConfigNameAlias(t *testing.T) {
	dir := t.TempDir()
	runModule(t, "logrotate", local(), map[string]any{
		"config_name": "nginx", "config_dir": dir,
		"paths": []string{"/var/log/nginx/*.log"}, "rotation_period": "daily",
	})
	if _, err := os.Stat(filepath.Join(dir, "nginx")); err != nil {
		t.Fatal(err)
	}
}

// A module's own warnings survive, after the alias ones.
func TestWithWarningsKeepsModuleWarnings(t *testing.T) {
	alias := []string{"Both option path and its alias dest are set."}
	for _, existing := range []any{[]any{"own"}, []string{"own"}} {
		res := withWarnings(Ok("").WithExtra("warnings", existing), alias)
		want := []any{alias[0], "own"}
		if !reflect.DeepEqual(res.Extra["warnings"], want) {
			t.Errorf("warnings = %#v, want %#v", res.Extra["warnings"], want)
		}
	}
	if res := withWarnings(Ok(""), nil); res.Extra != nil {
		t.Errorf("no warnings should add no key: %v", res.Extra)
	}
}
