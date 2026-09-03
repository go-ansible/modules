package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const systemdListUnitsOut = "sshd.service      loaded active running OpenSSH server\n" +
	"broken.service    not-found inactive dead   \n" +
	"-.mount           loaded active mounted Root Mount\n"

func TestModuleSystemdInfoAll(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl list-units --no-pager --type service,target,socket,mount,timer --all --plain --no-legend": {RC: 0, Stdout: systemdListUnitsOut},
		"systemctl show -p LoadState,ActiveState,SubState,FragmentPath,UnitFileState,UnitFilePreset,MainPID,ExecMainPID -- sshd.service": {
			RC: 0, Stdout: "LoadState=loaded\nActiveState=active\nSubState=running\nFragmentPath=/lib/systemd/system/sshd.service\n" +
				"UnitFileState=enabled\nUnitFilePreset=enabled\nMainPID=123\nExecMainPID=123\n",
		},
		"systemctl show -p LoadState,ActiveState,SubState,Where,What,Options,Type -- -.mount": {
			RC: 0, Stdout: "LoadState=loaded\nActiveState=active\nSubState=mounted\nWhere=/\nWhat=/dev/sda1\nOptions=rw\nType=ext4\n",
		},
	})
	res, err := moduleSystemdInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	units, ok := res.Extra["units"].(map[string]any)
	if !ok || len(units) != 3 {
		t.Fatalf("units = %#v", res.Extra["units"])
	}
	sshd, ok := units["sshd.service"].(map[string]any)
	if !ok || sshd["mainpid"] != "123" || sshd["loadstate"] != "loaded" {
		t.Fatalf("sshd = %#v", sshd)
	}
	broken, ok := units["broken.service"].(map[string]any)
	if !ok || broken["loadstate"] != "not-found" {
		t.Fatalf("broken = %#v", broken)
	}
	// not-found unit must get ONLY the minimal fields, no fragmentpath etc.
	if _, has := broken["fragmentpath"]; has {
		t.Fatalf("broken should not have fragmentpath: %#v", broken)
	}
	mnt, ok := units["-.mount"].(map[string]any)
	if !ok || mnt["where"] != "/" || mnt["type"] != "ext4" {
		t.Fatalf("mount = %#v", mnt)
	}
}

func TestModuleSystemdInfoSelectedUnit(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl list-units --no-pager --type service,target,socket,mount,timer --all --plain --no-legend": {RC: 0, Stdout: systemdListUnitsOut},
		"systemctl show -p LoadState,ActiveState,SubState,FragmentPath,UnitFileState,UnitFilePreset,MainPID,ExecMainPID -- sshd.service": {
			RC: 0, Stdout: "LoadState=loaded\nActiveState=active\nSubState=running\n",
		},
	})
	res, err := moduleSystemdInfo(context.Background(), conn, map[string]any{"unitname": []any{"sshd.service"}})
	if err != nil {
		t.Fatal(err)
	}
	units := res.Extra["units"].(map[string]any)
	if len(units) != 1 {
		t.Fatalf("units = %#v", units)
	}
}

func TestModuleSystemdInfoWildcard(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl list-units --no-pager --type service,target,socket,mount,timer --all --plain --no-legend": {RC: 0, Stdout: systemdListUnitsOut},
		"systemctl show -p LoadState,ActiveState,SubState,FragmentPath,UnitFileState,UnitFilePreset,MainPID,ExecMainPID -- sshd.service": {
			RC: 0, Stdout: "LoadState=loaded\nActiveState=active\nSubState=running\n",
		},
	})
	res, err := moduleSystemdInfo(context.Background(), conn, map[string]any{"unitname": []any{"sshd.*"}})
	if err != nil {
		t.Fatal(err)
	}
	units := res.Extra["units"].(map[string]any)
	if len(units) != 1 {
		t.Fatalf("units = %#v", units)
	}
}

func TestModuleSystemdInfoNoMatch(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl list-units --no-pager --type service,target,socket,mount,timer --all --plain --no-legend": {RC: 0, Stdout: systemdListUnitsOut},
	})
	res, err := moduleSystemdInfo(context.Background(), conn, map[string]any{"unitname": []any{"nope*"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed when no pattern matches")
	}
}

func TestModuleSystemdInfoExtraPropertyMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl list-units --no-pager --type service,target,socket,mount,timer --all --plain --no-legend": {RC: 0, Stdout: systemdListUnitsOut},
		"systemctl show -p LoadState,ActiveState,SubState,FragmentPath,UnitFileState,UnitFilePreset,MainPID,ExecMainPID,Description -- sshd.service": {
			RC: 0, Stdout: "LoadState=loaded\nActiveState=active\nSubState=running\n",
		},
	})
	res, err := moduleSystemdInfo(context.Background(), conn, map[string]any{
		"unitname": []any{"sshd.service"}, "extra_properties": []any{"Description"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed when an extra property is missing")
	}
}

func TestParseSystemdShowFirstWins(t *testing.T) {
	m := parseSystemdShow("Foo=1\nFoo=2\nBar=x\n")
	if m["foo"] != "1" {
		t.Fatalf("foo = %q, want first occurrence", m["foo"])
	}
	if m["bar"] != "x" {
		t.Fatalf("bar = %q", m["bar"])
	}
}
