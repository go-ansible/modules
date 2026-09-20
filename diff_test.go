package modules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInDiffMode(t *testing.T) {
	if InDiffMode(map[string]any{}) {
		t.Error("absent flag must not read as diff mode")
	}
	if InDiffMode(map[string]any{DiffModeKey: false}) {
		t.Error("false flag must not read as diff mode")
	}
	if !InDiffMode(map[string]any{DiffModeKey: true}) {
		t.Error("true flag must read as diff mode")
	}
	// A non-bool must not be coerced: a template that rendered the flag
	// to the string "false" would otherwise turn diff mode on.
	if InDiffMode(map[string]any{DiffModeKey: "false"}) {
		t.Error("a string must not read as diff mode")
	}
}

func TestContentDiffNewFile(t *testing.T) {
	// A nil before side is a file that does not exist. Its header must
	// stay empty, because real Ansible prints a bare "--- before" there.
	d := ContentDiff("/tmp/new.txt", nil, []byte("one\ntwo\n"))
	if d.Before != "" || d.BeforeHeader != "" {
		t.Errorf("before side = %q / header %q, want both empty", d.Before, d.BeforeHeader)
	}
	if d.After != "one\ntwo\n" {
		t.Errorf("after = %q", d.After)
	}
	if d.AfterHeader != "/tmp/new.txt (content)" {
		t.Errorf("after header = %q", d.AfterHeader)
	}
}

func TestContentDiffExistingEmptyFileIsNotAMissingOne(t *testing.T) {
	// An existing but empty file has a before side to compare, so it
	// gets a header; a missing one does not. Reading length instead of
	// nil-ness would conflate the two.
	d := ContentDiff("/tmp/f.txt", []byte{}, []byte("x\n"))
	if d.BeforeHeader != "/tmp/f.txt (content)" {
		t.Errorf("an existing empty file must still name its before side, got %q", d.BeforeHeader)
	}
}

func TestContentDiffBinaryAndOversize(t *testing.T) {
	binary := []byte("text\x00more")
	d := ContentDiff("/tmp/f.bin", binary, []byte("plain\n"))
	if !d.SrcBinary || d.Before != "" {
		t.Errorf("binary before side must be declined, got SrcBinary=%v Before=%q", d.SrcBinary, d.Before)
	}
	d = ContentDiff("/tmp/f.bin", []byte("plain\n"), binary)
	if !d.DstBinary || d.After != "" {
		t.Errorf("binary after side must be declined, got DstBinary=%v After=%q", d.DstBinary, d.After)
	}

	big := []byte(strings.Repeat("x", int(MaxDiffSize)+1))
	d = ContentDiff("/tmp/big", big, []byte("small\n"))
	if d.SrcLarger != MaxDiffSize || d.Before != "" {
		t.Errorf("oversize before side must be declined, got %d / %q", d.SrcLarger, d.Before)
	}
	d = ContentDiff("/tmp/big", []byte("small\n"), big)
	if d.DstLarger != MaxDiffSize || d.After != "" {
		t.Errorf("oversize after side must be declined, got %d / %q", d.DstLarger, d.After)
	}
}

func TestResultWithDiffIgnoresAnEmptyOne(t *testing.T) {
	// Modules attach unconditionally and let WithDiff decide, so a run
	// without --diff must come back carrying nothing.
	if got := Changed("x").WithDiff(Diff{}); got.Diffs != nil {
		t.Errorf("an empty diff must not be attached, got %+v", got.Diffs)
	}
	got := Changed("x").WithDiff(Diff{After: "a\n"}).WithDiff(Diff{After: "b\n"})
	if len(got.Diffs) != 2 {
		t.Fatalf("want 2 diffs, got %d", len(got.Diffs))
	}
	if got.Diffs[0].After != "a\n" || got.Diffs[1].After != "b\n" {
		t.Errorf("diffs out of order: %+v", got.Diffs)
	}
}

// The content modules only report a diff when asked, and report the real
// before/after contents when they are. Each is checked through the
// module's own entry point rather than through ContentDiff, so a module
// that forgets to call it fails here.
func TestContentModulesReportDiffsOnlyWhenAsked(t *testing.T) {
	type modCall func(dir string, args map[string]any) (Result, error)
	ctx := context.Background()
	conn := local()

	tests := []struct {
		name        string
		seed        string // "" means do not create the file
		args        func(path string) map[string]any
		run         modCall
		wantBefore  string
		wantAfter   string
		wantNoBefre bool // a created file has no before side at all
	}{{
		name: "copy",
		seed: "old\n",
		args: func(p string) map[string]any { return map[string]any{"dest": p, "content": "new\n"} },
		run: func(dir string, a map[string]any) (Result, error) {
			return moduleCopy(ctx, conn, a)
		},
		wantBefore: "old\n", wantAfter: "new\n",
	}, {
		name: "copy creating",
		args: func(p string) map[string]any { return map[string]any{"dest": p, "content": "new\n"} },
		run: func(dir string, a map[string]any) (Result, error) {
			return moduleCopy(ctx, conn, a)
		},
		wantAfter: "new\n", wantNoBefre: true,
	}, {
		name: "lineinfile",
		seed: "alpha\nbeta\n",
		args: func(p string) map[string]any { return map[string]any{"path": p, "line": "gamma"} },
		run: func(dir string, a map[string]any) (Result, error) {
			return moduleLineinfile(ctx, conn, a)
		},
		wantBefore: "alpha\nbeta\n", wantAfter: "alpha\nbeta\ngamma\n",
	}, {
		name: "lineinfile creating",
		args: func(p string) map[string]any {
			return map[string]any{"path": p, "line": "gamma", "create": true}
		},
		run: func(dir string, a map[string]any) (Result, error) {
			return moduleLineinfile(ctx, conn, a)
		},
		wantAfter: "gamma\n", wantNoBefre: true,
	}, {
		name: "replace",
		seed: "a\nfoo\nb\n",
		args: func(p string) map[string]any {
			return map[string]any{"path": p, "regexp": "foo", "replace": "bar"}
		},
		run: func(dir string, a map[string]any) (Result, error) {
			return moduleReplace(ctx, conn, a)
		},
		wantBefore: "a\nfoo\nb\n", wantAfter: "a\nbar\nb\n",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Without the flag, no diff comes back.
			dir := t.TempDir()
			path := filepath.Join(dir, "f.txt")
			if tt.seed != "" {
				if err := os.WriteFile(path, []byte(tt.seed), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			res, err := tt.run(dir, tt.args(path))
			if err != nil {
				t.Fatal(err)
			}
			if !res.Changed {
				t.Fatalf("setup wrong: module reported no change (%s)", res.Msg)
			}
			if res.Diffs != nil {
				t.Errorf("no --diff was asked for, yet the module reported %+v", res.Diffs)
			}

			// With the flag, the real contents come back.
			dir = t.TempDir()
			path = filepath.Join(dir, "f.txt")
			if tt.seed != "" {
				if err := os.WriteFile(path, []byte(tt.seed), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			args := tt.args(path)
			args[DiffModeKey] = true
			res, err = tt.run(dir, args)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Diffs) != 1 {
				t.Fatalf("want exactly one diff, got %d: %+v", len(res.Diffs), res.Diffs)
			}
			d := res.Diffs[0]
			if d.Before != tt.wantBefore {
				t.Errorf("before = %q, want %q", d.Before, tt.wantBefore)
			}
			if d.After != tt.wantAfter {
				t.Errorf("after = %q, want %q", d.After, tt.wantAfter)
			}
			wantAfterHeader := path + " (content)"
			if d.AfterHeader != wantAfterHeader {
				t.Errorf("after header = %q, want %q", d.AfterHeader, wantAfterHeader)
			}
			switch {
			case tt.wantNoBefre && d.BeforeHeader != "":
				t.Errorf("a created file must have no before header, got %q", d.BeforeHeader)
			case !tt.wantNoBefre && d.BeforeHeader != path+" (content)":
				t.Errorf("before header = %q, want %q", d.BeforeHeader, path+" (content)")
			}
		})
	}
}

// Check mode and diff mode compose: --diff --check must report the diff
// of a change it deliberately did not make.
func TestDiffInCheckModeLeavesTheFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := moduleLineinfile(context.Background(), local(), map[string]any{
		"path": path, "line": "beta",
		CheckModeKey: true, DiffModeKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("check mode must still report the change it would make")
	}
	if len(res.Diffs) != 1 || res.Diffs[0].After != "alpha\nbeta\n" {
		t.Fatalf("want the would-be contents, got %+v", res.Diffs)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "alpha\n" {
		t.Fatalf("check mode wrote to the file: %q", data)
	}
}

func TestTemplateReportsADiffOfTheRenderedResult(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "t.j2")
	dest := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(src, []byte("hello {{ who }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("hello nobody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := moduleTemplate(context.Background(), local(), map[string]any{
		"src": src, "dest": dest,
		"_vars":     map[string]any{"who": "world"},
		DiffModeKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diffs) != 1 {
		t.Fatalf("want one diff, got %+v", res.Diffs)
	}
	d := res.Diffs[0]
	// The diff must be of the RENDERED output, not of the template source.
	if d.Before != "hello nobody\n" || d.After != "hello world\n" {
		t.Errorf("before = %q, after = %q", d.Before, d.After)
	}
}

func TestBlockinfileReportsADiff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := moduleBlockinfile(context.Background(), local(), map[string]any{
		"path": path, "block": "inserted\n", DiffModeKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diffs) != 1 {
		t.Fatalf("want one diff, got %+v", res.Diffs)
	}
	d := res.Diffs[0]
	if d.Before != "keep\n" {
		t.Errorf("before = %q, want %q", d.Before, "keep\n")
	}
	if !strings.Contains(d.After, "inserted") || !strings.Contains(d.After, "BEGIN") {
		t.Errorf("after must carry the marked block, got %q", d.After)
	}
}
