package modules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeInterfacesFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "interfaces")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestModuleInterfacesFileAddNewOption(t *testing.T) {
	path := writeInterfacesFile(t, "auto eth1\niface eth1 inet static\n    address 192.168.1.1\n")
	conn := local()
	res, err := moduleInterfacesFile(context.Background(), conn, map[string]any{
		"dest": path, "iface": "eth1", "option": "mtu", "value": "8000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), "    mtu 8000\n") {
		t.Fatalf("content = %q", string(out))
	}
	ifaces := res.Extra["ifaces"].(map[string]any)
	eth1 := ifaces["eth1"].(map[string]any)
	if eth1["mtu"] != "8000" {
		t.Fatalf("eth1 = %#v", eth1)
	}
}

func TestModuleInterfacesFileUpdateExisting(t *testing.T) {
	path := writeInterfacesFile(t, "iface eth1 inet static\n    address 192.168.1.1\n    mtu 1500\n")
	conn := local()
	res, err := moduleInterfacesFile(context.Background(), conn, map[string]any{
		"dest": path, "iface": "eth1", "option": "mtu", "value": "8000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	out, _ := os.ReadFile(path)
	if strings.Contains(string(out), "1500") || !strings.Contains(string(out), "mtu 8000") {
		t.Fatalf("content = %q", string(out))
	}
}

func TestModuleInterfacesFileIdempotent(t *testing.T) {
	path := writeInterfacesFile(t, "iface eth1 inet static\n    mtu 8000\n")
	conn := local()
	res, err := moduleInterfacesFile(context.Background(), conn, map[string]any{
		"dest": path, "iface": "eth1", "option": "mtu", "value": "8000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when value already matches")
	}
}

func TestModuleInterfacesFileRepeatableOption(t *testing.T) {
	path := writeInterfacesFile(t, "iface eth1 inet static\n    up route add 1\n")
	conn := local()
	res, err := moduleInterfacesFile(context.Background(), conn, map[string]any{
		"dest": path, "iface": "eth1", "option": "up", "value": "route add 2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: new up value")
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), "route add 1") || !strings.Contains(string(out), "route add 2") {
		t.Fatalf("content = %q, want BOTH up lines preserved", string(out))
	}

	// adding the SAME up value again should be a no-op
	res2, err := moduleInterfacesFile(context.Background(), conn, map[string]any{
		"dest": path, "iface": "eth1", "option": "up", "value": "route add 2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Changed {
		t.Fatal("want unchanged: up value already present")
	}
}

func TestModuleInterfacesFileMethodRewrite(t *testing.T) {
	path := writeInterfacesFile(t, "iface eth1 inet static\n    address 1.2.3.4\n")
	conn := local()
	res, err := moduleInterfacesFile(context.Background(), conn, map[string]any{
		"dest": path, "iface": "eth1", "option": "method", "value": "dhcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), "iface eth1 inet dhcp\n") {
		t.Fatalf("content = %q", string(out))
	}
}

func TestModuleInterfacesFileAbsent(t *testing.T) {
	path := writeInterfacesFile(t, "iface eth1 inet static\n    mtu 8000\n")
	conn := local()
	res, err := moduleInterfacesFile(context.Background(), conn, map[string]any{
		"dest": path, "iface": "eth1", "option": "mtu", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	out, _ := os.ReadFile(path)
	if strings.Contains(string(out), "mtu") {
		t.Fatalf("content = %q", string(out))
	}
}

func TestModuleInterfacesFileBlankLineDoesNotEndStanza(t *testing.T) {
	path := writeInterfacesFile(t, "iface eth1 inet static\n    address 1.2.3.4\n\n    mtu 1500\n")
	ifaces := interfacesBuildFacts(interfacesParseLines(readFileString(t, path)))
	eth1 := ifaces["eth1"].(map[string]any)
	if eth1["mtu"] != "1500" || eth1["address"] != "1.2.3.4" {
		t.Fatalf("eth1 = %#v, want the blank line to NOT have ended the stanza", eth1)
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestModuleInterfacesFileIfaceNotFound(t *testing.T) {
	path := writeInterfacesFile(t, "iface eth1 inet static\n    mtu 8000\n")
	conn := local()
	res, err := moduleInterfacesFile(context.Background(), conn, map[string]any{
		"dest": path, "iface": "eth99", "option": "mtu", "value": "8000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed for unknown iface")
	}
}

func TestModuleInterfacesFileMissingIfaceArg(t *testing.T) {
	conn := local()
	_, err := moduleInterfacesFile(context.Background(), conn, map[string]any{
		"dest": "/tmp/whatever", "option": "mtu", "value": "8000",
	})
	if err == nil {
		t.Fatal("want error when option given without iface")
	}
}

func TestModuleInterfacesFileMissingValueArg(t *testing.T) {
	path := writeInterfacesFile(t, "iface eth1 inet static\n")
	conn := local()
	_, err := moduleInterfacesFile(context.Background(), conn, map[string]any{
		"dest": path, "iface": "eth1", "option": "mtu",
	})
	if err == nil {
		t.Fatal("want error when value missing and state=present")
	}
}

func TestModuleInterfacesFileDestMissing(t *testing.T) {
	conn := local()
	res, err := moduleInterfacesFile(context.Background(), conn, map[string]any{
		"dest": filepath.Join(t.TempDir(), "nope"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed when dest does not exist")
	}
}
