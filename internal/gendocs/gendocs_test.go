package gendocs

import (
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakePackage writes a miniature "modules" package into a temp dir.
// parser.ParseDir only parses, so the source need not type-check — but
// it must parse, which is why r is declared.
const fakePackage = `package modules

// moduleAlpha implements Ansible's ` + "`alpha`" + ` module: it does
// alpha things.
//
// Args: path (string, required).
func moduleAlpha() {}

// moduleBeta implements Ansible's ` + "`beta`" + ` module.
func moduleBeta() {}

// moduleArchive implements Ansible's ` + "`archive`" + ` module.
func moduleArchive() {}

func undocumented() {}

func Default() {
	var r registry
	r.Register("alpha", moduleAlpha)
	r.Register("beta", moduleBeta)
	r.Register("archive", moduleArchive)
	r.Register("quiet", undocumented)
}
`

func writeFake(t *testing.T, extra string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "registry.go"), []byte(fakePackage+extra), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGenerateShardsByInitialLetter(t *testing.T) {
	files, err := Generate(writeFake(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{IndexFile, "docs_generated_a.go", "docs_generated_b.go", "docs_generated_q.go"} {
		if _, ok := files[want]; !ok {
			t.Errorf("Generate produced no %s; it produced %v", want, names(files))
		}
	}
	if len(files) != 4 {
		t.Errorf("Generate produced %d files, want 4: %v", len(files), names(files))
	}

	// alpha and archive share an initial, so they share a shard —
	// that is the point of the split: adding a module rewrites one
	// file, not the whole table.
	a := string(files["docs_generated_a.go"])
	for _, want := range []string{`"alpha":`, `"archive":`, "var docsShardA = map[string]string{"} {
		if !strings.Contains(a, want) {
			t.Errorf("docs_generated_a.go does not contain %q", want)
		}
	}
	if strings.Contains(a, `"beta":`) {
		t.Error(`docs_generated_a.go contains "beta", which belongs in the b shard`)
	}
}

func TestGenerateIndexListsEveryShard(t *testing.T) {
	files, err := Generate(writeFake(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	index := string(files[IndexFile])
	for _, want := range []string{
		"var docsShards = []map[string]string{",
		"docsShardA,",
		"docsShardB,",
		"docsShardQ,",
		"var Docs = docsMerge(docsShards...)",
	} {
		if !strings.Contains(index, want) {
			t.Errorf("%s does not contain %q:\n%s", IndexFile, want, index)
		}
	}
}

func TestGenerateCarriesTheDocComment(t *testing.T) {
	files, err := Generate(writeFake(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	a := string(files["docs_generated_a.go"])
	if !strings.Contains(a, `Args: path (string, required).`) {
		t.Errorf("the alpha entry lost its Args line:\n%s", a)
	}
	// An undocumented function must say so rather than produce an
	// empty entry, which the modules package's coverage test would
	// then report as a missing doc with no hint of the cause.
	q := string(files["docs_generated_q.go"])
	if !strings.Contains(q, "(no documentation comment on undocumented)") {
		t.Errorf("the quiet entry does not name its undocumented function:\n%s", q)
	}
}

func TestGenerateBucketsNamesWithNoLowercaseInitial(t *testing.T) {
	dir := writeFake(t, "\nfunc more() {\n\tvar r registry\n\tr.Register(\"7zip\", moduleAlpha)\n}\n")
	files, err := Generate(dir)
	if err != nil {
		t.Fatal(err)
	}
	other, ok := files["docs_generated_other.go"]
	if !ok {
		t.Fatalf("Generate produced no docs_generated_other.go; it produced %v", names(files))
	}
	if !strings.Contains(string(other), `"7zip":`) {
		t.Errorf(`docs_generated_other.go does not contain "7zip":\n%s`, other)
	}
	if !strings.Contains(string(files[IndexFile]), "docsShardOther,") {
		t.Error("the index does not reference docsShardOther")
	}
}

func TestGenerateOutputIsGofmtClean(t *testing.T) {
	files, err := Generate(writeFake(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		want, err := format.Source(body)
		if err != nil {
			t.Errorf("%s does not parse: %v", name, err)
			continue
		}
		if string(want) != string(body) {
			t.Errorf("%s is not gofmt-clean, so `gofmt -l .` would fail on a freshly generated tree", name)
		}
	}
}

func TestGenerateRefusesAPackageWithNoRegistrations(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package modules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Generate(dir)
	if err == nil {
		t.Fatal("Generate accepted a package with no r.Register calls; an empty table would then be committed as if it were correct")
	}
	if !strings.Contains(err.Error(), "r.Register") {
		t.Errorf("the error does not name what is missing: %v", err)
	}
}

func TestGenerateRefusesADirectoryWithoutTheModulesPackage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(dir); err == nil {
		t.Fatal("Generate accepted a directory holding no modules package")
	}
}

// TestIsGeneratedSparesTheTestFile is the one case worth naming on its
// own: docs_generated_test.go begins with the shard prefix and ends in
// ".go", so a prefix match treats the test that guards these files as
// one of them — and the command deletes what it does not produce.
func TestIsGeneratedSparesTheTestFile(t *testing.T) {
	for _, tc := range []struct {
		base string
		want bool
	}{
		{"docs_generated.go", true},
		{"docs_generated_a.go", true},
		{"docs_generated_other.go", true},
		{"docs_generated_test.go", false},
		{"docs_generated_sync_test.go", false},
		{"docs.go", false},
		{"registry.go", false},
		{"setup.go", false},
	} {
		if got := IsGenerated(tc.base); got != tc.want {
			t.Errorf("IsGenerated(%q) = %v, want %v", tc.base, got, tc.want)
		}
	}
}

func names(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for name := range files {
		out = append(out, name)
	}
	return out
}
