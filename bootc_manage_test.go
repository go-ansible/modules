package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleBootcManageSwitchQueued(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"bootc switch example.com/image:latest --retain": {RC: 0, Stdout: "Queued for next boot: example.com/image:latest\n"},
	})
	res, err := moduleBootcManage(context.Background(), conn, map[string]any{
		"state": "switch", "image": "example.com/image:latest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleBootcManageSwitchUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"bootc switch example.com/image:latest --retain": {RC: 0, Stdout: "Image specification is unchanged.\n"},
	})
	res, err := moduleBootcManage(context.Background(), conn, map[string]any{
		"state": "switch", "image": "example.com/image:latest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleBootcManageLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"bootc upgrade": {RC: 0, Stdout: "No changes in registry\n"},
	})
	res, err := moduleBootcManage(context.Background(), conn, map[string]any{"state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleBootcManageSwitchMissingImage(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleBootcManage(context.Background(), conn, map[string]any{"state": "switch"}); err == nil {
		t.Fatal("want error: image required for state=switch")
	}
}

func TestModuleBootcManageInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleBootcManage(context.Background(), conn, map[string]any{"state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
