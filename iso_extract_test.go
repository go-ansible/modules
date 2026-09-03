package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIsoExtractVia7z(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /tmp/virt-rear": {RC: 0},
		"test -e /tmp/rear.iso":  {RC: 0},
		"command -v 7z":          {RC: 0},
		"mktemp -d":              {RC: 0, Stdout: "/tmp/xyz123\n"},
		"7z x /tmp/rear.iso -o/tmp/xyz123 isolinux/kernel":        {RC: 0},
		"test -e /tmp/xyz123/isolinux/kernel":                     {RC: 0},
		"test -e /tmp/virt-rear/kernel":                           {RC: 1},
		"cp -p /tmp/xyz123/isolinux/kernel /tmp/virt-rear/kernel": {RC: 0},
		"rm -rf /tmp/xyz123":                                      {RC: 0},
	})
	res, err := moduleIsoExtract(context.Background(), fc, map[string]any{
		"image": "/tmp/rear.iso", "dest": "/tmp/virt-rear", "files": []any{"isolinux/kernel"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	files, ok := res.Extra["files"].([]map[string]any)
	if !ok || len(files) != 1 || files[0]["dest"] != "/tmp/virt-rear/kernel" {
		t.Fatalf("files = %#v", res.Extra["files"])
	}
}

func TestModuleIsoExtractForceFalseSkipsExisting(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /tmp/virt-rear":        {RC: 0},
		"test -e /tmp/rear.iso":         {RC: 0},
		"test -e /tmp/virt-rear/kernel": {RC: 0},
	})
	res, err := moduleIsoExtract(context.Background(), fc, map[string]any{
		"image": "/tmp/rear.iso", "dest": "/tmp/virt-rear", "files": []any{"isolinux/kernel"}, "force": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: force=false and dest already exists")
	}
	for _, c := range fc.Commands {
		if c == "command -v 7z" || c == "mktemp -d" {
			t.Fatalf("commands = %v, want no extraction attempt", fc.Commands)
		}
	}
}

func TestModuleIsoExtractMountFallback(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /tmp/virt-rear":                              {RC: 0},
		"test -e /tmp/rear.iso":                               {RC: 0},
		"command -v 7z":                                       {RC: 1},
		"mktemp -d":                                           {RC: 0, Stdout: "/tmp/m1\n"},
		"mount -o loop,ro /tmp/rear.iso /tmp/m1":              {RC: 0},
		"test -e /tmp/m1/isolinux/kernel":                     {RC: 0},
		"test -e /tmp/virt-rear/kernel":                       {RC: 1},
		"cp -p /tmp/m1/isolinux/kernel /tmp/virt-rear/kernel": {RC: 0},
		"umount /tmp/m1":                                      {RC: 0},
		"rm -rf /tmp/m1":                                      {RC: 0},
	})
	res, err := moduleIsoExtract(context.Background(), fc, map[string]any{
		"image": "/tmp/rear.iso", "dest": "/tmp/virt-rear", "files": []any{"isolinux/kernel"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIsoExtractDestMissing(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /tmp/virt-rear": {RC: 1},
	})
	res, err := moduleIsoExtract(context.Background(), fc, map[string]any{
		"image": "/tmp/rear.iso", "dest": "/tmp/virt-rear", "files": []any{"isolinux/kernel"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when dest does not exist")
	}
}

func TestModuleIsoExtractMissingArgs(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIsoExtract(context.Background(), fc, map[string]any{"dest": "/x", "files": []any{"a"}}); err == nil {
		t.Fatal("want error for missing image")
	}
	if _, err := moduleIsoExtract(context.Background(), fc, map[string]any{"image": "/x.iso", "files": []any{"a"}}); err == nil {
		t.Fatal("want error for missing dest")
	}
	if _, err := moduleIsoExtract(context.Background(), fc, map[string]any{"image": "/x.iso", "dest": "/y"}); err == nil {
		t.Fatal("want error for missing/empty files")
	}
}
