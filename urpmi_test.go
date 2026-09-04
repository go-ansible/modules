package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleUrpmiInstall(t *testing.T) {
	// urpmiInstall re-checks query_package_provides after running urpmi
	// (matching real urpmi.py's own post-install verification), so the
	// whatprovides probe must answer "not provided" the first time and
	// "provided" the second — a static fakeConn can't express that,
	// hence seqConn.
	conn := newSeqConn(map[string][]remoteexec.Result{
		"rpm -q --whatprovides foo":                        {{RC: 1}, {RC: 0}},
		"urpmi --auto --force --quiet --no-recommends foo": {{RC: 0}},
	})
	res, err := moduleUrpmi(context.Background(), conn, map[string]any{"pkg": "foo", "state": "present"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleUrpmiInstallAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q --whatprovides foo": {RC: 0},
	})
	res, err := moduleUrpmi(context.Background(), conn, map[string]any{"pkg": "foo", "state": "present"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleUrpmiRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q foo":       {RC: 0},
		"urpme --auto foo": {RC: 0},
	})
	res, err := moduleUrpmi(context.Background(), conn, map[string]any{"pkg": "foo", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleUrpmiRemoveAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rpm -q foo": {RC: 1},
	})
	res, err := moduleUrpmi(context.Background(), conn, map[string]any{"pkg": "foo", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleUrpmiUpdateCache(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"urpmi.update -a -q":                               {{RC: 0}},
		"rpm -q --whatprovides bar":                        {{RC: 1}, {RC: 0}},
		"urpmi --auto --force --quiet --no-recommends bar": {{RC: 0}},
	})
	res, err := moduleUrpmi(context.Background(), conn, map[string]any{
		"name": "bar", "state": "present", "update_cache": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != "urpmi.update -a -q" {
		t.Fatalf("want update_cache to run first, commands = %v", conn.Commands)
	}
}

func TestModuleUrpmiEachPackageOwnToken(t *testing.T) {
	// Real urpmi's own module has a bug that mangles multi-package
	// installs into one garbled argv token (see the doc comment); this
	// port passes each pending package as its own token instead.
	conn := newSeqConn(map[string][]remoteexec.Result{
		"rpm -q --whatprovides foo":                            {{RC: 1}, {RC: 0}},
		"rpm -q --whatprovides bar":                            {{RC: 1}, {RC: 0}},
		"urpmi --auto --force --quiet --no-recommends foo bar": {{RC: 0}},
	})
	res, err := moduleUrpmi(context.Background(), conn, map[string]any{"name": []any{"foo", "bar"}, "state": "present"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, commands = %v", conn.Commands)
	}
}

func TestModuleUrpmiMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleUrpmi(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}
