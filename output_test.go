package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// TestFinalizeOutputMatchesRealAnsible pins the exact rstrip("\r\n")
// semantics measured against real ansible-core 2.21.4, which strips every
// trailing newline but no other whitespace and nothing leading.
func TestFinalizeOutputMatchesRealAnsible(t *testing.T) {
	cases := []struct {
		name      string
		stdout    string
		want      string
		wantLines []any
	}{
		{"one trailing newline", "hello\n", "hello", []any{"hello"}},
		{"several trailing newlines", "x\n\n\n", "x", []any{"x"}},
		{"embedded newline", "a\nb\n", "a\nb", []any{"a", "b"}},
		{"trailing spaces survive", "x  \t ", "x  \t ", []any{"x  \t "}},
		{"leading newlines survive", "\n\nx", "\n\nx", []any{"", "", "x"}},
		{"crlf", "a\r\nb\r\n", "a\r\nb", []any{"a", "b"}},
		{"empty", "", "", []any{}},
		{"only newlines", "\n\n", "", []any{}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := finalizeOutput(Ok("").WithExtra("stdout", c.stdout))
			if got.Extra["stdout"] != c.want {
				t.Errorf("stdout = %q, want %q", got.Extra["stdout"], c.want)
			}
			lines, ok := got.Extra["stdout_lines"].([]any)
			if !ok {
				t.Fatalf("stdout_lines = %#v, want []any", got.Extra["stdout_lines"])
			}
			if len(lines) != len(c.wantLines) {
				t.Fatalf("stdout_lines = %#v, want %#v", lines, c.wantLines)
			}
			for i := range lines {
				if lines[i] != c.wantLines[i] {
					t.Errorf("stdout_lines[%d] = %#v, want %#v", i, lines[i], c.wantLines[i])
				}
			}
		})
	}
}

// TestFinalizeOutputLeavesOtherFieldsAlone guards that the pass added to
// Registry.Run only touches stdout/stderr, and only when they are strings.
func TestFinalizeOutputLeavesOtherFieldsAlone(t *testing.T) {
	in := Ok("").WithExtra("cmd", []string{"echo", "hi\n"}).WithExtra("rc", 0).WithExtra("stdout", 42)
	got := finalizeOutput(in)
	if got.Extra["rc"] != 0 {
		t.Errorf("rc = %#v", got.Extra["rc"])
	}
	if got.Extra["stdout"] != 42 {
		t.Errorf("a non-string stdout was rewritten: %#v", got.Extra["stdout"])
	}
	if _, ok := got.Extra["stdout_lines"]; ok {
		t.Error("stdout_lines added for a non-string stdout")
	}
	if _, ok := got.Extra["stderr_lines"]; ok {
		t.Error("stderr_lines added when there was no stderr")
	}
}

// TestRegistryRunFinalizes proves the pass is actually wired into Run,
// not merely available.
func TestRegistryRunFinalizes(t *testing.T) {
	r := NewRegistry()
	r.Register("noisy", func(context.Context, remoteexec.Connection, map[string]any) (Result, error) {
		return Ok("").WithExtra("stdout", "a\nb\n").WithExtra("stderr", "e\n"), nil
	})
	res, err := r.Run(context.Background(), "noisy", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["stdout"] != "a\nb" {
		t.Errorf("stdout = %q, want %q", res.Extra["stdout"], "a\nb")
	}
	if res.Extra["stderr"] != "e" {
		t.Errorf("stderr = %q, want %q", res.Extra["stderr"], "e")
	}
	if lines, ok := res.Extra["stderr_lines"].([]any); !ok || len(lines) != 1 || lines[0] != "e" {
		t.Errorf("stderr_lines = %#v", res.Extra["stderr_lines"])
	}
}
