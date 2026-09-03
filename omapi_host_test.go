package modules

import (
	"context"
	"io"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// omshellFakeConn is a scripted Connection like fakeConn (see
// fakeconn_test.go), but keyed on the *stdin script* rather than the
// command line — omapi_host.go always runs the bare command "omshell"
// and drives it entirely through an omshell script piped on stdin, so
// fakeConn's own cmd-keyed map can't tell one invocation from another
// here.
type omshellFakeConn struct {
	on       map[string]remoteexec.Result
	Commands []string
}

func newOmshellFakeConn(on map[string]remoteexec.Result) *omshellFakeConn {
	return &omshellFakeConn{on: on}
}

func (f *omshellFakeConn) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	f.Commands = append(f.Commands, cmd)
	script := ""
	if stdin != nil {
		data, _ := io.ReadAll(stdin)
		script = string(data)
	}
	if res, ok := f.on[script]; ok {
		return res, nil
	}
	return remoteexec.Result{}, nil
}

func (f *omshellFakeConn) Put(ctx context.Context, localPath, remotePath string, opts remoteexec.PutOptions) error {
	return nil
}
func (f *omshellFakeConn) Fetch(ctx context.Context, remotePath, localPath string) error { return nil }
func (f *omshellFakeConn) Remove(ctx context.Context, remotePath string) error           { return nil }
func (f *omshellFakeConn) TempPath(base string) string                                   { return "/tmp/" + base }
func (f *omshellFakeConn) Close() error                                                  { return nil }

var _ remoteexec.Connection = (*omshellFakeConn)(nil)

const omapiPreamble = "server 10.98.4.55\n" +
	"port 7911\n" +
	"key defomapi +bFQtBCta6j2vWkjPkNFtgA==\n" +
	"connect\n" +
	"new host\n"

func TestModuleOmapiHostCreate(t *testing.T) {
	lookup := omapiPreamble + "set hardware-address = 44:dd:ab:dd:11:44\nopen\n"
	create := omapiPreamble +
		"set name = \"server01\"\n" +
		"set hardware-address = 44:dd:ab:dd:11:44\n" +
		"set hardware-type = 1\n" +
		"set ip-address = 192.168.88.99\n" +
		"set statements = \"ddns-hostname \\\"server01\\\"; filename \\\"pxelinux.0\\\"; next-server 1.1.1.1\"\n" +
		"create\n"

	conn := newOmshellFakeConn(map[string]remoteexec.Result{
		lookup: {RC: 0, Stdout: "can't open object: no match.\n"},
		create: {RC: 0, Stdout: "server01: obj: host\n"},
	})
	res, err := moduleOmapiHost(context.Background(), conn, map[string]any{
		"key_name": "defomapi",
		"key":      "+bFQtBCta6j2vWkjPkNFtgA==",
		"host":     "10.98.4.55",
		"macaddr":  "44:dd:ab:dd:11:44",
		"name":     "server01",
		"ip":       "192.168.88.99",
		"ddns":     true,
		"statements": []any{
			`filename "pxelinux.0"`, "next-server 1.1.1.1",
		},
		"state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleOmapiHostAlreadyPresent(t *testing.T) {
	lookup := omapiPreamble + "set hardware-address = 44:dd:ab:dd:11:44\nopen\n"
	conn := newOmshellFakeConn(map[string]remoteexec.Result{
		lookup: {RC: 0, Stdout: "obj: host\n  name = server01\n"},
	})
	res, err := moduleOmapiHost(context.Background(), conn, map[string]any{
		"key_name": "defomapi",
		"key":      "+bFQtBCta6j2vWkjPkNFtgA==",
		"host":     "10.98.4.55",
		"macaddr":  "44:dd:ab:dd:11:44",
		"name":     "server01",
		"state":    "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: host already exists")
	}
}

func TestModuleOmapiHostRemove(t *testing.T) {
	lookup := "server 10.1.1.1\n" +
		"port 7911\n" +
		"key defomapi +bFQtBCta6j2vWkjPkNFtgA==\n" +
		"connect\n" +
		"new host\n" +
		"set hardware-address = 00:66:ab:dd:11:44\nopen\n"
	remove := lookup + "remove\n"
	conn := newOmshellFakeConn(map[string]remoteexec.Result{
		lookup: {RC: 0, Stdout: "obj: host\n"},
		remove: {RC: 0, Stdout: "host removed.\n"},
	})
	res, err := moduleOmapiHost(context.Background(), conn, map[string]any{
		"key_name": "defomapi",
		"key":      "+bFQtBCta6j2vWkjPkNFtgA==",
		"host":     "10.1.1.1",
		"macaddr":  "00:66:ab:dd:11:44",
		"state":    "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOmapiHostRemoveAbsent(t *testing.T) {
	lookup := "server 10.1.1.1\n" +
		"port 7911\n" +
		"key defomapi +bFQtBCta6j2vWkjPkNFtgA==\n" +
		"connect\n" +
		"new host\n" +
		"set hardware-address = 00:66:ab:dd:11:44\nopen\n"
	conn := newOmshellFakeConn(map[string]remoteexec.Result{
		lookup: {RC: 0, Stdout: "can't open object: no match.\n"},
	})
	res, err := moduleOmapiHost(context.Background(), conn, map[string]any{
		"key_name": "defomapi",
		"key":      "+bFQtBCta6j2vWkjPkNFtgA==",
		"host":     "10.1.1.1",
		"macaddr":  "00:66:ab:dd:11:44",
		"state":    "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: already absent")
	}
}

func TestModuleOmapiHostMissingArgs(t *testing.T) {
	conn := newOmshellFakeConn(nil)
	_, err := moduleOmapiHost(context.Background(), conn, map[string]any{"state": "present"})
	if err == nil {
		t.Fatal("want error for missing key_name/key/macaddr")
	}
}
