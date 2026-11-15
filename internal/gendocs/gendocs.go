// Package gendocs extracts each registered module's Go doc comment
// (already written on its module<Name> function — this project's
// convention has always been to document arguments and every
// deviation from real Ansible's behavior there) and produces the
// docs_generated*.go sources: a map from each module's registered
// name to that comment text, for ansible-doc to print without needing
// the source tree at runtime.
//
// It is a library, not just a command, so that a test in the modules
// package can regenerate into memory and compare against what is
// committed. That comparison is the only thing that catches a
// committed copy which has gone STALE: a module whose doc comment was
// edited without running `go generate` keeps its key, so a test that
// only checks every registered name has an entry sees nothing wrong.
//
// Run via `go generate ./...` from the modules package (see the
// //go:generate directive in registry.go).
package gendocs

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
)

// MaxShardBytes is the ceiling each generated file is held under, and
// it exists because of a measurement rather than a preference: the
// fleet push guard refuses to publish a blob of 1 MiB or more, and the
// single docs_generated.go this package replaced had reached 1 367 610
// bytes. Every pull request that registered a module therefore needed
// GITSAFE_ALLOW_BIGBLOB=1 to be pushed at all — a guard that has to be
// switched off for routine work stops being read (go-ansible/modules#142).
//
// Half a mebibyte leaves the largest shard today several times its
// current size in headroom, and still refuses well before the push
// guard would: a test that names the file which grew is a far better
// report than a refused push at the end of an afternoon's work.
//
// The ceiling is asserted by a test in the modules package, not by
// Generate. A generator that refused to write would leave no way
// forward at all — you could not even produce the file you then have
// to subdivide.
const MaxShardBytes = 512 << 10

// IndexFile is the generated file that declares Docs itself. The
// entries live in the shards beside it.
const IndexFile = "docs_generated.go"

const shardPrefix = "docs_generated_"

// otherShard collects any registered name that does not begin with an
// ASCII lowercase letter. No module is named that way today; the
// bucket exists so that one appearing does not silently land in a
// file named after a character that is awkward on a case-insensitive
// or non-ASCII filesystem.
const otherShard = "other"

// IsGenerated reports whether base (a file's base name, not a path) is
// one of the files Generate owns and the command may therefore
// overwrite or delete.
//
// ⛔ The _test.go exclusion is the whole reason this is a function
// rather than a prefix match at each call site: the package's own
// docs_generated_test.go begins with the shard prefix and ends in
// ".go", so a naive pattern matches it and the generator deletes the
// test that guards it.
func IsGenerated(base string) bool {
	if strings.HasSuffix(base, "_test.go") {
		return false
	}
	if base == IndexFile {
		return true
	}
	return strings.HasPrefix(base, shardPrefix) && strings.HasSuffix(base, ".go")
}

// Generate parses the modules package in dir and returns the content
// of every file it owns, keyed by base name. Nothing is written: the
// caller either writes the files (the command) or compares them with
// what is committed (the test).
func Generate(dir string) (map[string][]byte, error) {
	funcDocs, regs, err := scan(dir)
	if err != nil {
		return nil, err
	}

	shards := map[string][]registration{}
	for _, r := range regs {
		shards[shardKey(r.name)] = append(shards[shardKey(r.name)], r)
	}
	keys := make([]string, 0, len(shards))
	for k := range shards {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make(map[string][]byte, len(keys)+1)
	for _, k := range keys {
		src, err := shardSource(k, shards[k], funcDocs)
		if err != nil {
			return nil, err
		}
		out[shardFile(k)] = src
	}
	index, err := indexSource(keys)
	if err != nil {
		return nil, err
	}
	out[IndexFile] = index
	return out, nil
}

type registration struct{ name, ident string }

// scan reads the package's doc comments and its r.Register calls.
func scan(dir string) (funcDocs map[string]string, regs []registration, err error) {
	fset := token.NewFileSet()
	notTest := func(info os.FileInfo) bool { return !strings.HasSuffix(info.Name(), "_test.go") }
	pkgs, err := parser.ParseDir(fset, dir, notTest, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}
	pkg, ok := pkgs["modules"]
	if !ok {
		return nil, nil, fmt.Errorf("no %q package found in %s", "modules", dir)
	}

	// funcDocs: Go identifier (e.g. "moduleSetup") -> its doc comment.
	funcDocs = map[string]string{}
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Doc == nil {
				continue
			}
			funcDocs[fn.Name.Name] = cleanDoc(fn.Doc.Text())
		}
	}

	// regs: registered name -> Go identifier, read from every
	// r.Register("name", identifier) call anywhere in the package
	// (registry.go holds them all today, but scanning the whole
	// package means a future split file still works).
	for _, file := range pkg.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Register" || len(call.Args) != 2 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			name, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			ident, ok := call.Args[1].(*ast.Ident)
			if !ok {
				return true
			}
			regs = append(regs, registration{name: name, ident: ident.Name})
			return true
		})
	}
	if len(regs) == 0 {
		return nil, nil, fmt.Errorf("found no r.Register(%q, moduleFn) calls in %s", "name", dir)
	}
	sort.Slice(regs, func(i, j int) bool { return regs[i].name < regs[j].name })
	return funcDocs, regs, nil
}

func shardKey(name string) string {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return otherShard
	}
	return name[:1]
}

func shardFile(key string) string { return shardPrefix + key + ".go" }

func shardVar(key string) string {
	return "docsShard" + strings.ToUpper(key[:1]) + key[1:]
}

func header(b *strings.Builder) {
	b.WriteString("// Code generated by modules/internal/gendocs. DO NOT EDIT.\n")
	b.WriteString("// Run `go generate ./...` from the modules package to regenerate\n")
	b.WriteString("// after adding, removing, or re-documenting a module.\n\n")
	b.WriteString("package modules\n\n")
}

func shardSource(key string, regs []registration, funcDocs map[string]string) ([]byte, error) {
	var b strings.Builder
	header(&b)
	fmt.Fprintf(&b, "// %s holds the doc comments of the registered modules whose\n", shardVar(key))
	if key == otherShard {
		b.WriteString("// names do not begin with an ASCII lowercase letter.\n")
	} else {
		fmt.Fprintf(&b, "// names begin with %q. See gendocs.MaxShardBytes for why the\n", key)
		b.WriteString("// table is split by initial letter rather than kept in one file.\n")
	}
	fmt.Fprintf(&b, "var %s = map[string]string{\n", shardVar(key))
	for _, r := range regs {
		doc, ok := funcDocs[r.ident]
		if !ok {
			doc = "(no documentation comment on " + r.ident + ")\n"
		}
		fmt.Fprintf(&b, "\t%q: %q,\n", r.name, doc)
	}
	b.WriteString("}\n")
	return format.Source([]byte(b.String()))
}

func indexSource(keys []string) ([]byte, error) {
	var b strings.Builder
	header(&b)
	b.WriteString("// docsShards are the per-initial-letter tables Docs is built from.\n")
	b.WriteString("// Keeping them in a slice — rather than naming each one in the\n")
	b.WriteString("// expression below — lets a test sum their lengths and so notice a\n")
	b.WriteString("// name present in two shards, which merging would silently hide.\n")
	b.WriteString("var docsShards = []map[string]string{\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "\t%s,\n", shardVar(k))
	}
	b.WriteString("}\n\n")
	b.WriteString("// Docs maps every registered module name to its Go doc comment —\n")
	b.WriteString("// the same argument/deviation documentation this project has always\n")
	b.WriteString("// written on each module<Name> function, extracted once at build\n")
	b.WriteString("// time so ansible-doc can print it without the source tree present.\n")
	b.WriteString("//\n")
	b.WriteString("// The entries live in the docs_generated_<letter>.go shards beside\n")
	b.WriteString("// this file; see gendocs.MaxShardBytes for the measurement that put\n")
	b.WriteString("// them there.\n")
	b.WriteString("var Docs = docsMerge(docsShards...)\n")
	return format.Source([]byte(b.String()))
}

// cleanDoc normalizes a doc comment to exactly one trailing newline
// (ast.CommentGroup.Text already strips comment markers and leading
// whitespace) — kept as a named step in case future formatting wants a
// hook here.
func cleanDoc(text string) string {
	return strings.TrimRight(text, "\n") + "\n"
}
