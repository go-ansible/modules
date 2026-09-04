package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleImgadmImport(t *testing.T) {
	uuid := "70e3ae72-96b6-11e6-9056-9737fd4d0764"
	conn := newFakeConn(map[string]remoteexec.Result{
		"imgadm import -P zones -q " + uuid: {RC: 0, Stdout: "Imported image " + uuid + " (some-image@1.0.0)\n"},
	})
	res, err := moduleImgadm(context.Background(), conn, map[string]any{"uuid": uuid, "state": "imported"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleImgadmImportAlreadyInstalled(t *testing.T) {
	uuid := "70e3ae72-96b6-11e6-9056-9737fd4d0764"
	conn := newFakeConn(map[string]remoteexec.Result{
		"imgadm import -P zones -q " + uuid: {RC: 0, Stdout: "Image " + uuid + " (some-image@1.0.0) is already installed, skipping\n"},
	})
	res, err := moduleImgadm(context.Background(), conn, map[string]any{"uuid": uuid, "state": "present"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleImgadmDelete(t *testing.T) {
	uuid := "70e3ae72-96b6-11e6-9056-9737fd4d0764"
	conn := newFakeConn(map[string]remoteexec.Result{
		"imgadm delete -P zones " + uuid: {RC: 0, Stdout: "Deleted image " + uuid + "\n"},
	})
	res, err := moduleImgadm(context.Background(), conn, map[string]any{"uuid": uuid, "state": "deleted"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleImgadmUpdateAlwaysChanged(t *testing.T) {
	uuid := "70e3ae72-96b6-11e6-9056-9737fd4d0764"
	conn := newFakeConn(map[string]remoteexec.Result{
		"imgadm update " + uuid: {RC: 0},
	})
	res, err := moduleImgadm(context.Background(), conn, map[string]any{"uuid": uuid, "state": "updated"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: imgadm gives no signal, so update always reports changed")
	}
}

func TestModuleImgadmVacuum(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"imgadm vacuum -f": {RC: 0, Stdout: ""},
	})
	res, err := moduleImgadm(context.Background(), conn, map[string]any{"uuid": "*", "state": "vacuumed"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when vacuum stdout is empty")
	}
	for _, c := range conn.Commands {
		if c != "imgadm vacuum -f" {
			t.Fatalf("want only the vacuum command to run, not %q (see the doc comment about real imgadm's own fall-through bug)", c)
		}
	}
}

func TestModuleImgadmAddSource(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"imgadm sources -a https://docker.io -t docker": {RC: 0, Stdout: `Added "docker" image source "https://docker.io"` + "\n"},
	})
	res, err := moduleImgadm(context.Background(), conn, map[string]any{
		"source": "https://docker.io", "type": "docker", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleImgadmRemoveSource(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"imgadm sources -d https://docker.io": {RC: 0, Stdout: `Deleted "docker" image source "https://docker.io"` + "\n"},
	})
	res, err := moduleImgadm(context.Background(), conn, map[string]any{
		"source": "https://docker.io", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleImgadmInvalidUUID(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleImgadm(context.Background(), conn, map[string]any{"uuid": "not-a-uuid", "state": "present"}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleImgadmWildcardOnlyForUpdatedOrVacuumed(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleImgadm(context.Background(), conn, map[string]any{"uuid": "*", "state": "present"}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleImgadmMissingState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleImgadm(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}
