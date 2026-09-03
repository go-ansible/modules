package modules

import (
	"context"
	"io"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// apache2SeqConn is a fakeConn variant that returns each command's
// results in sequence (one per call, sticking on the last once
// exhausted) rather than a single fixed value per command — needed to
// test apache2_module's own "re-verify `<ctl> -M` after running
// a2enmod/a2dismod" flow, where the SAME command is expected to report
// a DIFFERENT enabled/disabled state on its second call (the module's
// own state actually having been changed by a2enmod/a2dismod in
// between, on a real target). fakeConn itself (fakeconn_test.go) only
// supports one fixed Result per command and is shared across the whole
// package's tests, so this is a local, additive variant rather than a
// change to it.
type apache2SeqConn struct {
	on       map[string][]remoteexec.Result
	Commands []string
	calls    map[string]int
}

func (f *apache2SeqConn) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	f.Commands = append(f.Commands, cmd)
	seq, ok := f.on[cmd]
	if !ok || len(seq) == 0 {
		return remoteexec.Result{}, nil
	}
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	i := f.calls[cmd]
	if i >= len(seq) {
		i = len(seq) - 1
	}
	f.calls[cmd]++
	return seq[i], nil
}

func (f *apache2SeqConn) Put(ctx context.Context, localPath, remotePath string, opts remoteexec.PutOptions) error {
	return nil
}
func (f *apache2SeqConn) Fetch(ctx context.Context, remotePath, localPath string) error { return nil }
func (f *apache2SeqConn) Remove(ctx context.Context, remotePath string) error           { return nil }
func (f *apache2SeqConn) TempPath(base string) string                                   { return "/tmp/" + base }
func (f *apache2SeqConn) Close() error                                                  { return nil }

var _ remoteexec.Connection = (*apache2SeqConn)(nil)

func TestModuleApache2ModuleEnable(t *testing.T) {
	fc := &apache2SeqConn{on: map[string][]remoteexec.Result{
		"command -v apache2ctl": {{RC: 0}},
		"apache2ctl -M": {
			{RC: 0, Stdout: "Loaded Modules:\n rewrite_module (shared)\n"},               // before: disabled
			{RC: 0, Stdout: "Loaded Modules:\n rewrite_module (shared)\n wsgi_module\n"}, // after: enabled
		},
		"command -v a2enmod": {{RC: 0}},
		"a2enmod wsgi":       {{RC: 0}},
	}}
	res, err := moduleApache2Module(context.Background(), fc, map[string]any{"name": "wsgi", "state": "present"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleApache2ModuleAlreadyEnabled(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v apache2ctl": {RC: 0},
		"apache2ctl -M":         {RC: 0, Stdout: "Loaded Modules:\n wsgi_module (shared)\n"},
	})
	res, err := moduleApache2Module(context.Background(), fc, map[string]any{"name": "wsgi", "state": "present"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when module is already enabled")
	}
}

func TestModuleApache2ModuleDisableForce(t *testing.T) {
	fc := &apache2SeqConn{on: map[string][]remoteexec.Result{
		"command -v apache2ctl": {{RC: 0}},
		"apache2ctl -M": {
			{RC: 0, Stdout: "Loaded Modules:\n autoindex_module (shared)\n"}, // before: enabled
			{RC: 0, Stdout: "Loaded Modules:\n rewrite_module (shared)\n"},   // after: disabled
		},
		"command -v a2dismod":   {{RC: 0}},
		"a2dismod -f autoindex": {{RC: 0}},
	}}
	res, err := moduleApache2Module(context.Background(), fc, map[string]any{
		"name": "autoindex", "state": "absent", "force": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleApache2ModuleCustomIdentifier(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v apache2ctl": {RC: 0},
		"apache2ctl -M":         {RC: 0, Stdout: "Loaded Modules:\n dumpio_module (shared)\n"},
	})
	res, err := moduleApache2Module(context.Background(), fc, map[string]any{
		"name": "dump_io", "state": "present", "identifier": "dumpio_module",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: dumpio_module is already listed")
	}
}

func TestModuleApache2ModuleCgiThreadedFails(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v apache2ctl": {RC: 0},
		"apache2ctl -V":         {RC: 0, Stdout: "Server MPM: event\n  threaded: yes\n"},
	})
	res, err := moduleApache2Module(context.Background(), fc, map[string]any{"name": "cgi", "state": "present"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for cgi under a threaded MPM")
	}
}

func TestModuleApache2ModuleNoCtlBinary(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v apache2ctl": {RC: 1},
		"command -v apachectl":  {RC: 1},
	})
	res, err := moduleApache2Module(context.Background(), fc, map[string]any{"name": "wsgi"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when no apache control binary is found")
	}
}

func TestCreateApache2Identifier(t *testing.T) {
	cases := map[string]string{
		"wsgi":    "wsgi_module",
		"shib2":   "mod_shib",
		"evasive": "evasive20_module",
		"php8":    "php_module",
		"php7.4":  "php7_module",
	}
	for name, want := range cases {
		if got := createApache2Identifier(name); got != want {
			t.Errorf("createApache2Identifier(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestModuleApache2ModuleMissingName(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleApache2Module(context.Background(), fc, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
