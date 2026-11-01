package modules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// TestEveryCheckModeImplementationIsRegistered is the invariant whose
// absence let five modules implement a dry run the engine never reached:
// checkModeSupport is opt-in, so a module can grow a CORRECT check-mode
// branch and still be skipped, with nothing anywhere saying so. Both
// halves are silent, which is why this reads the package's own sources
// rather than trusting a hand-kept list.
//
// It asks each REGISTERED module about its OWN function body:
//
//   - the registry hands out a Func, and runtime.FuncForPC turns that
//     into the Go name the function was declared under;
//   - the package's sources are parsed, and that function's body is
//     walked for a call to InCheckMode.
//
// The earlier version of this test matched a FILE's basename against a
// module name, which made it blame whoever happened to share a file
// name. pagerduty.go holds three modules: pagerduty_change and
// pagerduty_alert branch on check mode (real declares
// supports_check_mode=True for both) while `pagerduty` does not (real
// declares nothing), and the file-name version reported `pagerduty` --
// a module with no such branch -- as the offender. Asking the function
// instead of the file cannot make that mistake.
//
// What it still cannot see: a branch taken inside a HELPER the module
// calls rather than in the module's own body. That is an
// under-approximation, so the control below fails if the walk stops
// finding branches at all.
func TestEveryCheckModeImplementationIsRegistered(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Go name -> does its body call InCheckMode.
	branches := map[string]bool{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || fn.Body == nil {
					continue
				}
				found := false
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "InCheckMode" {
						found = true
						return false
					}
					return true
				})
				if found {
					branches[fn.Name.Name] = true
				}
			}
		}
	}
	if len(branches) == 0 {
		t.Fatal("no function in this package calls InCheckMode: the parse or the walk has stopped seeing, " +
			"which reads exactly like a package with no check-mode support")
	}

	var checked int
	for _, name := range Default().Names() {
		fn, ok := Default().Get(name)
		if !ok {
			t.Errorf("%s is listed by Names() but Get() does not find it", name)
			continue
		}
		goName := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
		if i := strings.LastIndexByte(goName, '.'); i >= 0 {
			goName = goName[i+1:]
		}
		if !branches[goName] {
			continue
		}
		checked++
		if !SupportsCheckMode(name) {
			t.Errorf("%s (%s) branches on check mode but is absent from checkModeSupport, "+
				"so the engine skips it instead of running the branch", name, goName)
		}
	}

	// The positive control: a scan that cannot see is indistinguishable
	// from a scan that found nothing. A dozen modules implement this
	// branch, so a count in single digits means the function lookup or
	// the walk stopped matching, not that the gap closed.
	if checked < 10 {
		t.Errorf("only %d registered module(s) were found to branch on check mode; "+
			"the source walk has stopped seeing them", checked)
	}
}
