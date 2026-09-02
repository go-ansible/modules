package modules

import (
	"context"
	"errors"
	"io"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestArgError(t *testing.T) {
	err := errArg("bad %s", "thing")
	if err.Error() != "bad thing" {
		t.Fatalf("Error() = %q", err.Error())
	}
}

func TestArgStringNonString(t *testing.T) {
	got := argString(map[string]any{"k": 42}, "k", "def")
	if got != "42" {
		t.Fatalf("argString = %q", got)
	}
}

func TestRequireStringWrongType(t *testing.T) {
	if _, err := requireString(map[string]any{"k": 42}, "k"); err == nil {
		t.Fatal("want error for non-string value")
	}
}

func TestRequireStringEmpty(t *testing.T) {
	if _, err := requireString(map[string]any{"k": ""}, "k"); err == nil {
		t.Fatal("want error for empty string")
	}
}

func TestArgBool(t *testing.T) {
	cases := []struct {
		v    any
		def  bool
		want bool
	}{
		{true, false, true},
		{"true", false, true},
		{"false", true, false},
		{"not-a-bool", true, true}, // falls back to def
		{nil, true, true},          // key absent (checked separately below)
	}
	for _, c := range cases {
		args := map[string]any{}
		if c.v != nil {
			args["k"] = c.v
		}
		if got := argBool(args, "k", c.def); got != c.want {
			t.Errorf("argBool(%v, def=%v) = %v, want %v", c.v, c.def, got, c.want)
		}
	}
}

func TestArgInt(t *testing.T) {
	cases := []struct {
		v    any
		want int
	}{
		{5, 5},
		{int64(6), 6},
		{float64(7), 7},
		{"8", 8},
		{"not-a-number", 99}, // falls back to def
	}
	for _, c := range cases {
		got := argInt(map[string]any{"k": c.v}, "k", 99)
		if got != c.want {
			t.Errorf("argInt(%v) = %d, want %d", c.v, got, c.want)
		}
	}
	if got := argInt(map[string]any{}, "k", 99); got != 99 {
		t.Errorf("argInt(missing) = %d, want 99", got)
	}
}

func TestArgStringListVariants(t *testing.T) {
	if got := argStringList(map[string]any{"k": []string{"a", "b"}}, "k"); len(got) != 2 {
		t.Errorf("[]string form: got %v", got)
	}
	if got := argStringList(map[string]any{"k": []any{"a", 1}}, "k"); len(got) != 2 || got[1] != "1" {
		t.Errorf("[]any form: got %v", got)
	}
	if got := argStringList(map[string]any{"k": "solo"}, "k"); len(got) != 1 || got[0] != "solo" {
		t.Errorf("string form: got %v", got)
	}
	if got := argStringList(map[string]any{"k": 5}, "k"); got != nil {
		t.Errorf("unrecognized type: got %v, want nil", got)
	}
	if got := argStringList(map[string]any{}, "k"); got != nil {
		t.Errorf("missing key: got %v, want nil", got)
	}
}

func TestArgModeInvalid(t *testing.T) {
	if _, err := argMode(map[string]any{"mode": "not-octal"}, "mode"); err == nil {
		t.Fatal("want error for invalid octal mode")
	}
}

func TestArgModeMissing(t *testing.T) {
	m, err := argMode(map[string]any{}, "mode")
	if err != nil {
		t.Fatal(err)
	}
	if m != nil {
		t.Fatal("want nil for unset mode")
	}
}

func TestArgModeNonStringValue(t *testing.T) {
	// 644 (int, not string) is formatted via fmt.Sprint into "644" and
	// parses as valid octal, exercising the non-string branch of
	// argMode without needing an invalid digit.
	m, err := argMode(map[string]any{"mode": 644}, "mode")
	if err != nil {
		t.Fatal(err)
	}
	if m == nil || *m != 0o644 {
		t.Fatalf("m = %v, want 0644", m)
	}
}

// errConn is a Connection whose every method fails — for exercising the
// transport-error branches of helpers built on top of Exec/Put/Fetch.
type errConn struct{}

func (errConn) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	return remoteexec.Result{}, errors.New("boom")
}
func (errConn) Put(ctx context.Context, localPath, remotePath string, opts remoteexec.PutOptions) error {
	return errors.New("boom")
}
func (errConn) Fetch(ctx context.Context, remotePath, localPath string) error {
	return errors.New("boom")
}
func (errConn) Remove(ctx context.Context, remotePath string) error { return errors.New("boom") }
func (errConn) TempPath(base string) string                         { return "/tmp/" + base }
func (errConn) Close() error                                        { return nil }

var _ remoteexec.Connection = errConn{}

func TestRunTransportError(t *testing.T) {
	if _, err := run(context.Background(), errConn{}, "x"); err == nil {
		t.Fatal("want error")
	}
}

func TestPathExistsTransportError(t *testing.T) {
	if _, err := pathExists(context.Background(), errConn{}, "/x"); err == nil {
		t.Fatal("want error")
	}
}

func TestRunNonZeroExit(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{"false": {RC: 1, Stderr: "nope"}})
	if _, err := run(context.Background(), conn, "false"); err == nil {
		t.Fatal("want error for non-zero exit")
	}
}
