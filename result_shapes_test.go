package modules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

// Real's template reports EXACTLY the key set real's copy reports --
// measured side by side on the same dest:
//
//	copy      changed,checksum,dest,failed,gid,group,md5sum,mode,owner,size,src,state,uid
//	template  changed,checksum,dest,failed,gid,group,md5sum,mode,owner,size,src,state,uid
//
// and no msg on either. This module returned Changed(dest), so three
// keys came back instead of thirteen and msg held the DESTINATION
// PATH. A playbook reading r.dest, r.checksum or r.mode after a
// template -- ordinary usage -- got an undefined variable.
func TestTemplateReportsTheSameKeysAsCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "s.j2")
	if err := os.WriteFile(src, []byte("hi {{ 1 + 1 }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "d.txt")
	res, err := Default().Run(context.Background(), "template", remoteexec.NewLocal(),
		map[string]any{"src": src, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoMsg {
		t.Errorf("template emits a msg key (it held %q); real emits none", res.Msg)
	}
	for _, k := range []string{"dest", "checksum", "md5sum", "mode", "owner", "group", "size", "src", "state", "uid", "gid"} {
		if _, ok := res.Extra[k]; !ok {
			t.Errorf("missing key %q that real reports", k)
		}
	}
	if res.Extra["state"] != "file" {
		t.Errorf("state = %v, want file", res.Extra["state"])
	}
	// and the template was actually rendered, not copied verbatim
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hi 2\n" {
		t.Errorf("rendered %q, want %q", got, "hi 2\n")
	}
}

// known_hosts ends with `results = copy.copy(module.params)` in real,
// so every parameter comes back as a result key. Real's own source
// carries "# TODO: deprecate returning everything that was passed in"
// beside it -- undesirable upstream or not, it is the behaviour.
//
// `path` in Extra is also what makes addPathInfo fill in the file
// attributes, and it OVERWRITES state with the file's own: measured,
// real reports state=file here, not the parameter's "present".
func TestKnownHostsEchoesItsParameters(t *testing.T) {
	dir := t.TempDir()
	kh := filepath.Join(dir, "kh")
	if err := os.WriteFile(kh, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Default().Run(context.Background(), "known_hosts", remoteexec.NewLocal(),
		map[string]any{"path": kh, "name": "example.com", "key": "example.com ssh-rsa AAAAB3NzaC1yc2E="})
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoMsg {
		t.Errorf("known_hosts emits a msg key (it held %q); real emits none", res.Msg)
	}
	for k, want := range map[string]any{
		"name": "example.com", "path": kh, "hash_host": false,
		"key": "example.com ssh-rsa AAAAB3NzaC1yc2E=",
	} {
		if got := res.Extra[k]; got != want {
			t.Errorf("%s = %v, want %v", k, got, want)
		}
	}
	// state is the PARAMETER here; addPathInfo replaces it with the
	// file's own in Registry.Run, which is what real reports. This test
	// calls Run, so it sees the replaced one.
	if res.Extra["state"] != "file" {
		t.Errorf("state = %v, want file (addPathInfo overwrites the parameter)", res.Extra["state"])
	}
	for _, k := range []string{"mode", "owner", "group", "size", "uid", "gid"} {
		if _, ok := res.Extra[k]; !ok {
			t.Errorf("missing %q -- addPathInfo did not fire, so `path` is not in Extra", k)
		}
	}
}

// set_fact's real action plugin is `result['ansible_facts'] = facts;
// return result` and sets no msg, so `r.msg` after a real set_fact is
// undefined. This reported "facts set" -- a sentence real never emits.
func TestSetFactReturnsNoMsg(t *testing.T) {
	res, err := Default().Run(context.Background(), "set_fact", remoteexec.NewLocal(),
		map[string]any{"k": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoMsg {
		t.Errorf("set_fact emits a msg key (it held %q); real emits none", res.Msg)
	}
	if res.Facts["k"] != "v" {
		t.Errorf("facts = %v", res.Facts)
	}
	if res.Changed {
		t.Error("set_fact reported changed; real never does")
	}
}

// Real's pause reports twelve keys and no msg. Measured for
// `pause: {seconds: 1}`:
//
//	rc=0  echo=True  delta=1  user_input=''  stderr=''
//	stdout="Paused for 1.02 seconds"
//
// delta is the INT seconds; stdout carries the real elapsed to two
// decimals. They are not the same number, so reusing one for both
// would be wrong in whichever place it was reused.
func TestPauseReportsRealsKeys(t *testing.T) {
	res, err := Default().Run(context.Background(), "pause", remoteexec.NewLocal(),
		map[string]any{"seconds": 1})
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoMsg {
		t.Errorf("pause emits a msg key (it held %q); real emits none", res.Msg)
	}
	for _, k := range []string{"start", "stop", "delta", "echo", "rc", "user_input", "stdout", "stderr"} {
		if _, ok := res.Extra[k]; !ok {
			t.Errorf("missing %q that real reports", k)
		}
	}
	// finalizeOutput derives these from stdout/stderr for every module
	for _, k := range []string{"stdout_lines", "stderr_lines"} {
		if _, ok := res.Extra[k]; !ok {
			t.Errorf("missing %q -- finalizeOutput did not fire", k)
		}
	}
	if res.Extra["delta"] != 1 {
		t.Errorf("delta = %v, want the int seconds", res.Extra["delta"])
	}
	if s, _ := res.Extra["stdout"].(string); !strings.HasPrefix(s, "Paused for ") {
		t.Errorf("stdout = %q, want real's own wording", s)
	}
	if res.Changed {
		t.Error("pause reported changed; real reports false")
	}
}
