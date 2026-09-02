package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePackageFactsAptAuto(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		pkgMgrProbe: {RC: 0, Stdout: "apt"},
		`dpkg-query -W -f='${Package} ${Version}\n'`: {
			RC: 0, Stdout: "curl 7.68.0-1\nbash 5.0-6\n",
		},
	})
	res, err := modulePackageFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	pkgs, ok := res.Extra["packages"].(map[string]any)
	if !ok {
		t.Fatalf("Extra[packages] = %#v, want a map", res.Extra["packages"])
	}
	entries, ok := pkgs["curl"].([]map[string]any)
	if !ok || len(entries) != 1 || entries[0]["version"] != "7.68.0-1" {
		t.Fatalf("pkgs[curl] = %#v", pkgs["curl"])
	}
}

func TestModulePackageFactsDnfExplicit(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`rpm -qa --qf '%{NAME} %{VERSION}-%{RELEASE}\n'`: {
			RC: 0, Stdout: "curl 7.76.1-14\n",
		},
	})
	res, err := modulePackageFacts(context.Background(), conn, map[string]any{"manager": "dnf"})
	if err != nil {
		t.Fatal(err)
	}
	pkgs := res.Extra["packages"].(map[string]any)
	entries := pkgs["curl"].([]map[string]any)
	if entries[0]["version"] != "7.76.1-14" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestModulePackageFactsYumAndRpmAliases(t *testing.T) {
	for _, mgr := range []string{"yum", "rpm", "dnf5"} {
		conn := newFakeConn(map[string]remoteexec.Result{
			`rpm -qa --qf '%{NAME} %{VERSION}-%{RELEASE}\n'`: {RC: 0, Stdout: "git 1-1\n"},
		})
		res, err := modulePackageFacts(context.Background(), conn, map[string]any{"manager": mgr})
		if err != nil {
			t.Fatal(err)
		}
		pkgs := res.Extra["packages"].(map[string]any)
		if _, ok := pkgs["git"]; !ok {
			t.Fatalf("manager=%s: pkgs = %#v", mgr, pkgs)
		}
	}
}

func TestModulePackageFactsMultipleVersions(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`rpm -qa --qf '%{NAME} %{VERSION}-%{RELEASE}\n'`: {
			RC: 0, Stdout: "kernel 5.14-1\nkernel 5.15-1\n",
		},
	})
	res, err := modulePackageFacts(context.Background(), conn, map[string]any{"manager": "rpm"})
	if err != nil {
		t.Fatal(err)
	}
	pkgs := res.Extra["packages"].(map[string]any)
	entries := pkgs["kernel"].([]map[string]any)
	if len(entries) != 2 {
		t.Fatalf("entries = %#v, want 2", entries)
	}
}

func TestModulePackageFactsNoManagerFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		pkgMgrProbe: {RC: 0, Stdout: "none"},
	})
	res, err := modulePackageFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: no package manager found")
	}
}

func TestModulePackageFactsCommandFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`rpm -qa --qf '%{NAME} %{VERSION}-%{RELEASE}\n'`: {RC: 1, Stderr: "boom"},
	})
	res, err := modulePackageFacts(context.Background(), conn, map[string]any{"manager": "dnf"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed")
	}
}

func TestModulePackageFactsUnsupportedManager(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePackageFacts(context.Background(), conn, map[string]any{"manager": "pacman"}); err == nil {
		t.Fatal("want error for unsupported manager")
	}
}

func TestModulePackageFactsSkipsMalformedLines(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`dpkg-query -W -f='${Package} ${Version}\n'`: {
			RC: 0, Stdout: "curl 1.0\n\nmalformed\n",
		},
	})
	res, err := modulePackageFacts(context.Background(), conn, map[string]any{"manager": "apt"})
	if err != nil {
		t.Fatal(err)
	}
	pkgs := res.Extra["packages"].(map[string]any)
	if len(pkgs) != 1 {
		t.Fatalf("pkgs = %#v, want 1 entry", pkgs)
	}
}
