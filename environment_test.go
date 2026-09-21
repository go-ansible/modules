package modules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvironmentPrefix(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]any
		want string
	}{
		{"none", nil, ""},
		{"empty", map[string]any{}, ""},
		// shellQuote leaves a value that needs no quoting alone.
		{"one", map[string]any{"A": "b"}, "export A=b; "},
		// Sorted, so the same task composes the same line twice.
		{"sorted", map[string]any{"B": "2", "A": "1"}, "export A=1 B=2; "},
		// Real Ansible renders a YAML bool through Python's str().
		{"bool true", map[string]any{"F": true}, "export F=True; "},
		{"bool false", map[string]any{"F": false}, "export F=False; "},
		{"number", map[string]any{"N": 42}, "export N=42; "},
		{"nil", map[string]any{"N": nil}, "export N=None; "},
		// Quoting has to survive a value that contains a quote.
		{"quotes", map[string]any{"S": "it's"}, `export S='it'"'"'s'; `},
		{"spaces", map[string]any{"S": "a b"}, "export S='a b'; "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{}
			if tt.env != nil {
				args[EnvironmentKey] = tt.env
			}
			if got := EnvironmentPrefix(args); got != tt.want {
				t.Errorf("EnvironmentPrefix = %q, want %q", got, tt.want)
			}
		})
	}
}

// The prefix must reach both command and shell, and must come BEFORE a
// chdir — a `cd x && cmd` with assignments written in front would set
// them for the cd alone.
func TestEnvironmentReachesTheCommand(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	ctx := context.Background()

	for _, tt := range []struct {
		name, module string
		args         map[string]any
	}{
		{"shell", "shell", map[string]any{"cmd": "echo $MY_PROBE > " + out}},
		{"command", "command", map[string]any{"_raw_params": "sh -c 'echo $MY_PROBE > " + out + "'"}},
		{"shell with chdir", "shell", map[string]any{"cmd": "echo $MY_PROBE > " + out, "chdir": dir}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Remove(out)
			args := map[string]any{}
			for k, v := range tt.args {
				args[k] = v
			}
			args[EnvironmentKey] = map[string]any{"MY_PROBE": "reached"}

			line, skip, _, err := ComposeCommandLine(ctx, local(), tt.module, args)
			if err != nil || skip {
				t.Fatalf("compose: err=%v skip=%v", err, skip)
			}
			if !strings.HasPrefix(line, "export MY_PROBE=reached; ") {
				t.Fatalf("prefix missing or misplaced: %q", line)
			}
			if _, err := local().Exec(ctx, line, nil); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(string(data)) != "reached" {
				t.Errorf("the command saw %q, want reached", strings.TrimSpace(string(data)))
			}
		})
	}
}
