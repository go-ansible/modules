package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// Three modules returned a `msg` key real does not return, or returned
// the wrong value in it. Every expectation here was measured by running
// the same task through a real ansible-playbook and through this port's
// binary, comparing the registered result's full key set:
//
//	slurp     real: changed,content,encoding,failed,source
//	          ours: changed,content,encoding,failed,msg   <- msg held the PATH
//	stat      real: changed,failed,stat
//	          ours: changed,failed,msg,stat               <- msg was ""
//	assemble  msg="OK" in real, "" here
//
// The distinction between an absent msg and an empty one is not
// pedantry: `r.msg` is an UNDEFINED variable after a real stat, and ""
// after a real command. Result.NoMsg exists for exactly this, and these
// three were not using it.

// slurp's real exit_json is
// `module.exit_json(content=data, source=source, encoding=encoding)` --
// source included, msg absent.
func TestSlurpReturnsSourceAndNoMsg(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Default().Run(context.Background(), "slurp", remoteexec.NewLocal(),
		map[string]any{"src": p})
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoMsg {
		t.Errorf("slurp emits a msg key; real emits none (it held %q)", res.Msg)
	}
	if got := res.Extra["source"]; got != p {
		t.Errorf("source = %v, want %q -- real returns it and this did not", got, p)
	}
	if res.Extra["content"] != "aGVsbG8=" {
		t.Errorf("content = %v, want the base64 of hello", res.Extra["content"])
	}
	if res.Extra["encoding"] != "base64" {
		t.Errorf("encoding = %v", res.Extra["encoding"])
	}
}

// stat's real exit_json is `module.exit_json(changed=False, stat=output)`
// and nothing else.
func TestStatReturnsNoMsg(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Default().Run(context.Background(), "stat", remoteexec.NewLocal(),
		map[string]any{"path": p})
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoMsg {
		t.Error("stat emits a msg key; after a real stat r.msg is undefined")
	}
	if _, ok := res.Extra["stat"]; !ok {
		t.Error("no stat dictionary")
	}
}

// assemble ends with a literal result['msg'] = "OK" before exit_json,
// changed or not.
func TestAssembleReportsOK(t *testing.T) {
	dir := t.TempDir()
	parts := filepath.Join(dir, "parts")
	if err := os.MkdirAll(parts, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"01": "1\n", "02": "2\n"} {
		if err := os.WriteFile(filepath.Join(parts, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dest := filepath.Join(dir, "out.txt")
	res, err := Default().Run(context.Background(), "assemble", remoteexec.NewLocal(),
		map[string]any{"src": parts, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg != "OK" {
		t.Errorf("msg = %q, want %q", res.Msg, "OK")
	}
	if !res.Changed {
		t.Error("assembling a file that did not exist reported no change")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "1\n2\n" {
		t.Errorf("assembled %q", got)
	}
}

// Real attaches `exception` to every FAILED module result and to no
// successful one. Measured across eight failures in six modules --
// fail, assert, file, command, uri, copy, unarchive, find -- where the
// value is always the literal string below and never an actual
// traceback. raw.go used to carry a note saying the key could not be
// reproduced because it "holds a Python traceback"; that premise was
// wrong, and the note now records the measurement instead.
func TestFailedResultsCarryException(t *testing.T) {
	res, err := Default().Run(context.Background(), "fail", remoteexec.NewLocal(),
		map[string]any{"msg": "stop"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("fail did not fail")
	}
	if got := res.Extra["exception"]; got != "(traceback unavailable)" {
		t.Errorf("exception = %v, want real's own literal", got)
	}

	// and a success carries no such key
	ok, err := Default().Run(context.Background(), "debug", remoteexec.NewLocal(),
		map[string]any{"msg": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := ok.Extra["exception"]; present {
		t.Error("a successful result carries an exception key")
	}
}

// AddException is idempotent: a module that set the key itself keeps
// its own value.
func TestAddExceptionDoesNotOverwrite(t *testing.T) {
	in := Fail("boom").WithExtra("exception", "mine")
	if got := AddException(in).Extra["exception"]; got != "mine" {
		t.Errorf("exception = %v, want the module's own value kept", got)
	}
}
