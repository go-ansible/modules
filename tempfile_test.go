package modules

import (
	"context"
	"os"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestTempfileCmd(t *testing.T) {
	cmd, err := tempfileCmd("file", "/tmp", "ansible.", "")
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "mktemp /tmp/ansible.XXXXXX" {
		t.Fatalf("cmd = %q", cmd)
	}

	cmd, err = tempfileCmd("directory", "/var/tmp/", "pre.", ".suf")
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "mktemp -d /var/tmp/pre.XXXXXX.suf" {
		t.Fatalf("cmd = %q", cmd)
	}

	if _, err := tempfileCmd("bogus", "/tmp", "a", ""); err == nil {
		t.Fatal("want error for invalid state")
	}
}

func TestModuleTempfileFile(t *testing.T) {
	cmd, err := tempfileCmd("file", "/tmp", "ansible.", "")
	if err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 0, Stdout: "/tmp/ansible.aB3f9K\n"},
	})
	res, err := moduleTempfile(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["path"] != "/tmp/ansible.aB3f9K" {
		t.Fatalf("path = %v", res.Extra["path"])
	}
	if res.Extra["state"] != "file" {
		t.Fatalf("state = %v", res.Extra["state"])
	}
}

func TestModuleTempfileDirectory(t *testing.T) {
	cmd, err := tempfileCmd("directory", "/tmp", "custom.", ".x")
	if err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 0, Stdout: "/tmp/custom.Zz9988.x\n"},
	})
	res, err := moduleTempfile(context.Background(), conn, map[string]any{
		"state": "directory", "prefix": "custom.", "suffix": ".x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["path"] != "/tmp/custom.Zz9988.x" {
		t.Fatalf("path = %v", res.Extra["path"])
	}
}

func TestModuleTempfileInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleTempfile(context.Background(), conn, map[string]any{"state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
}

func TestModuleTempfileMktempError(t *testing.T) {
	cmd, err := tempfileCmd("file", "/tmp", "ansible.", "")
	if err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 1, Stderr: "mktemp: failed"},
	})
	if _, err := moduleTempfile(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error when mktemp fails")
	}
}

// Measured from real: tempfile reports the new file's ownership and
// mode and NO msg. Keys: changed failed gid group mode owner path size
// state uid, with mode a STRING ("0600" for a file, "0700" for a
// directory) and size an int.
func TestModuleTempfileResultShape(t *testing.T) {
	ctx := context.Background()
	conn := local()

	for _, tc := range []struct{ state, wantMode string }{
		{"file", "0600"},
		{"directory", "0700"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			res, err := moduleTempfile(ctx, conn, map[string]any{"state": tc.state})
			if err != nil {
				t.Fatal(err)
			}
			path, _ := res.Extra["path"].(string)
			if path == "" {
				t.Fatal("no path reported")
			}
			defer os.RemoveAll(path)

			if !res.NoMsg {
				t.Error("tempfile reported a msg key; real has none")
			}
			if res.Extra["state"] != tc.state {
				t.Errorf("state = %v, want %v", res.Extra["state"], tc.state)
			}
			if got, ok := res.Extra["mode"].(string); !ok || got != tc.wantMode {
				t.Errorf("mode = %#v, want the string %q", res.Extra["mode"], tc.wantMode)
			}
			for _, k := range []string{"uid", "gid", "owner", "group", "size"} {
				if _, has := res.Extra[k]; !has {
					t.Errorf("tempfile did not report %q", k)
				}
			}
		})
	}
}
