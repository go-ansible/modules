package modules

import (
	"context"
	"os"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// TestExpandArgumentVarsAgainstARealShell runs each case through a real
// shell, because the whole design delegates the expansion there -- a
// table of expected SHELL TEXT would only assert that the generator
// equals itself, and would not catch a form the shell reads differently
// from how I read it.
func TestExpandArgumentVarsAgainstARealShell(t *testing.T) {
	t.Setenv("GA_SET", "value-of-set")
	os.Unsetenv("GA_UNSET")

	for _, tc := range []struct{ name, arg, want string }{
		{"a set variable expands", "$GA_SET", "value-of-set"},
		{"braced form too", "${GA_SET}", "value-of-set"},
		{"embedded in text", "x$GA_SET/y", "xvalue-of-set/y"},
		// Real: "if a variable is not matched, it is left unchanged,
		// unlike shell substitution which would remove it." A bare
		// "$GA_UNSET" in a shell would give the empty string.
		{"an UNSET variable stays literal", "$GA_UNSET", "$GA_UNSET"},
		{"an unset braced form stays literal", "${GA_UNSET}", "$GA_UNSET"},
		// ⛔ None of these may be interpreted. A glob, a command
		// substitution, a backtick and a word split are all things the
		// shell would do to an unquoted argument and expandvars does
		// not do at all.
		{"a glob is literal", "*", "*"},
		{"a command substitution is literal", "$(touch /tmp/ga-pwned)", "$(touch /tmp/ga-pwned)"},
		{"a backtick is literal", "`id`", "`id`"},
		{"a semicolon is literal", "a; id", "a; id"},
		{"a quote is literal", `it's`, `it's`},
		{"a positional is literal", "$1", "$1"},
		{"a lone dollar is literal", "$", "$"},
		{"a shell default form stays literal", "${GA_SET:-x}", "${GA_SET:-x}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// printf %s so nothing the shell prints is of our own making.
			cmd := "printf %s " + expandArgumentVars(tc.arg)
			res, err := remoteexec.NewLocal().Exec(context.Background(), cmd, nil)
			if err != nil {
				t.Fatal(err)
			}
			if res.RC != 0 {
				t.Fatalf("the generated command did not run: rc=%d stderr=%q cmd=%s", res.RC, res.Stderr, cmd)
			}
			if res.Stdout != tc.want {
				t.Errorf("got %q, want %q\n  generated: %s", res.Stdout, tc.want, cmd)
			}
		})
	}
}

// The control for the table above: the SAME arguments unquoted really
// are interpreted by a shell, so "literal" is a property of the
// generator and not of the shell being harmless.
func TestTheShellWouldHaveInterpretedThem(t *testing.T) {
	for _, tc := range []struct{ arg, mustNotEqual string }{
		{"*", "*"},
		{"$HOME", "$HOME"},
	} {
		res, err := remoteexec.NewLocal().Exec(context.Background(), "printf '%s' "+tc.arg, nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.Stdout == tc.mustNotEqual {
			t.Errorf("control failed: the shell left %q alone even unquoted, so the table above proves nothing", tc.arg)
		}
	}
}

// A home directory expands, and only at the start -- expanduser does
// not treat "a~b" as a path.
func TestTildeExpandsOnlyAtTheStart(t *testing.T) {
	home := os.Getenv("HOME")
	if home == "" {
		t.Skip("no HOME to expand")
	}
	for _, tc := range []struct{ arg, want string }{
		{"~", home},
		{"~/x", home + "/x"},
		{"a~b", "a~b"},
	} {
		res, err := remoteexec.NewLocal().Exec(context.Background(), "printf %s "+expandArgumentVars(tc.arg), nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.Stdout != tc.want {
			t.Errorf("%q -> %q, want %q", tc.arg, res.Stdout, tc.want)
		}
	}
}

// expand_argument_vars: false turns it off, which is what the option is
// for -- and the generated command must then carry the text literally.
func TestExpandArgumentVarsCanBeDisabled(t *testing.T) {
	t.Setenv("GA_SET", "value-of-set")
	conn := remoteexec.NewLocal()

	on, err := Default().Run(context.Background(), "command", conn,
		map[string]any{"argv": []any{"printf", "%s", "$GA_SET"}})
	if err != nil {
		t.Fatal(err)
	}
	off, err := Default().Run(context.Background(), "command", conn,
		map[string]any{"argv": []any{"printf", "%s", "$GA_SET"}, "expand_argument_vars": false})
	if err != nil {
		t.Fatal(err)
	}
	if on.Extra["stdout"] != "value-of-set" {
		t.Errorf("default (true) did not expand: %q", on.Extra["stdout"])
	}
	if off.Extra["stdout"] != "$GA_SET" {
		t.Errorf("expand_argument_vars: false expanded anyway: %q", off.Extra["stdout"])
	}
}

// ⛔ The injection case, end to end through the module rather than
// through the helper: an argument that would run a command if it
// reached a shell unquoted must leave no trace.
func TestAnArgumentCannotRunACommand(t *testing.T) {
	dir := t.TempDir()
	probe := dir + "/pwned"
	_, err := Default().Run(context.Background(), "command", remoteexec.NewLocal(),
		map[string]any{"argv": []any{"printf", "%s", "$(touch " + probe + ")"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(probe); err == nil {
		t.Fatal("an argv element ran a command")
	}
	// The control: the same payload DOES fire through a shell, so the
	// absence above is the quoting rather than a dud payload.
	ctl := dir + "/control"
	if _, err := remoteexec.NewLocal().Exec(context.Background(),
		"printf %s $(touch "+ctl+")", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ctl); err != nil {
		t.Fatalf("control failed: the payload did not fire even unquoted (%v)", err)
	}
}
