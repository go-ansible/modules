package modules

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Measured from real ansible-core 2.21.4. Three shapes, one per module
// family -- which is why they are built separately rather than from one
// rule, and why this test checks each against its own measurement.
//
//	lineinfile  [ {before, after, before_header "<p> (content)",
//	                after_header "<p> (content)"},
//	              {before_header "<p> (file attributes)",
//	                after_header "<p> (file attributes)"} ]
//	blockinfile the same
//	ini_file    a single DICT shaped like the content entry
//	copy        a LIST, empty unless diff is on
//	replace     absent
func TestDiffKeyShapes(t *testing.T) {
	const p = "/tmp/x.txt"

	want := []any{
		map[string]any{
			"before": "", "after": "",
			"before_header": "/tmp/x.txt (content)",
			"after_header":  "/tmp/x.txt (content)",
		},
		map[string]any{
			"before_header": "/tmp/x.txt (file attributes)",
			"after_header":  "/tmp/x.txt (file attributes)",
		},
	}
	if got := fileDiffKey(p, "", ""); !reflect.DeepEqual(got, want) {
		t.Errorf("fileDiffKey =\n %#v\nwant %#v", got, want)
	}

	// ini_file's is the content entry ALONE, not wrapped in a list.
	if got := contentDiffEntry(p, "a", "b"); !reflect.DeepEqual(got, map[string]any{
		"before": "a", "after": "b",
		"before_header": "/tmp/x.txt (content)",
		"after_header":  "/tmp/x.txt (content)",
	}) {
		t.Errorf("contentDiffEntry = %#v", got)
	}

	// copy's headers are the BARE path, and the list is empty when diff
	// is off.
	if got := copyDiffKey(p, "a", "b", false); !reflect.DeepEqual(got, []any{}) {
		t.Errorf("copyDiffKey off = %#v, want an empty list", got)
	}
	if got := copyDiffKey(p, "a", "b", true); !reflect.DeepEqual(got, []any{map[string]any{
		"before_header": p, "before": "a", "after_header": p, "after": "b",
	}}) {
		t.Errorf("copyDiffKey on = %#v", got)
	}

	// before and after are EMPTY unless diff is on: real fills them only
	// then, so an ordinary run carries the shape and not the content.
	if b, a := diffContent(false, []byte("x"), []byte("y")); b != "" || a != "" {
		t.Errorf("diffContent(off) = %q, %q, want empty", b, a)
	}
	if b, a := diffContent(true, []byte("x"), []byte("y")); b != "x" || a != "y" {
		t.Errorf("diffContent(on) = %q, %q", b, a)
	}
}

// Real carries these keys whether or not anything changed, and whether
// or not diff is on -- measured on both paths of all three modules.
func TestDiffKeyIsPresentOnEveryPath(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	conn := local()

	f := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(f, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		run     func() (Result, error)
		wantKey []string
	}{
		{"lineinfile changed", func() (Result, error) {
			return moduleLineinfile(ctx, conn, map[string]any{"path": f, "line": "beta"})
		}, []string{"diff", "backup"}},
		{"lineinfile unchanged", func() (Result, error) {
			return moduleLineinfile(ctx, conn, map[string]any{"path": f, "line": "beta"})
		}, []string{"diff", "backup"}},
		{"blockinfile changed", func() (Result, error) {
			return moduleBlockinfile(ctx, conn, map[string]any{"path": f, "block": "b"})
		}, []string{"diff"}},
		{"blockinfile unchanged", func() (Result, error) {
			return moduleBlockinfile(ctx, conn, map[string]any{"path": f, "block": "b"})
		}, []string{"diff"}},
		{"ini_file changed", func() (Result, error) {
			return moduleIniFile(ctx, conn, map[string]any{
				"path": filepath.Join(dir, "i.ini"), "section": "s", "option": "k", "value": "v", "create": true})
		}, []string{"diff", "path", "state", "mode"}},
		{"ini_file unchanged", func() (Result, error) {
			return moduleIniFile(ctx, conn, map[string]any{
				"path": filepath.Join(dir, "i.ini"), "section": "s", "option": "k", "value": "v", "create": true})
		}, []string{"diff", "path", "state", "mode"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := tc.run()
			if err != nil {
				t.Fatal(err)
			}
			for _, k := range tc.wantKey {
				if _, ok := res.Extra[k]; !ok {
					t.Errorf("no %q key: %#v", k, res.Extra)
				}
			}
			// Real keeps a msg key on all of these, empty or not.
			if res.NoMsg {
				t.Error("NoMsg set; real reports a msg key")
			}
			// And the SHAPE, not merely the presence: ini_file's diff is
			// a single dict where lineinfile's and blockinfile's is a
			// list of two. Without this a neuter that gave ini_file the
			// list shape passed.
			switch d := res.Extra["diff"].(type) {
			case []any:
				if strings.HasPrefix(tc.name, "ini_file") {
					t.Errorf("ini_file's diff is a list; real's is a single dict")
				} else if len(d) != 2 {
					t.Errorf("diff has %d entries, want 2 (content, file attributes)", len(d))
				}
			case map[string]any:
				if !strings.HasPrefix(tc.name, "ini_file") {
					t.Errorf("%s diff is a dict; real's is a list of two", tc.name)
				}
				if _, ok := d["before_header"]; !ok {
					t.Errorf("diff dict has no before_header: %#v", d)
				}
			default:
				t.Errorf("diff is %T", res.Extra["diff"])
			}
		})
	}
}
