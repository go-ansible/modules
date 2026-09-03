package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKopiaRepositoryInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kopia repository status --config-file=/etc/kopia/root.config": {
			RC: 0, Stdout: "Connected to repository: s3:/my-bucket/\nConfig file: /etc/kopia/root.config\n",
		},
		"kopia repository throttle get --config-file=/etc/kopia/root.config": {
			RC: 0, Stdout: "upload-bytes-per-second: 0\ndownload-bytes-per-second: 0\n",
		},
	})
	res, err := moduleKopiaRepositoryInfo(context.Background(), conn, map[string]any{
		"config": "/etc/kopia/root.config",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
	if res.Extra["repository_status"] == "" {
		t.Fatal("want repository_status populated")
	}
	if res.Extra["throttle"] == "" {
		t.Fatal("want throttle populated")
	}
}

func TestModuleKopiaRepositoryInfoStatusFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kopia repository status": {RC: 1, Stderr: "not connected to a repository"},
	})
	res, err := moduleKopiaRepositoryInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when repository status fails")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("want throttle get never attempted after a status failure, commands = %v", conn.Commands)
	}
}

func TestModuleKopiaRepositoryInfoThrottleFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kopia repository status":       {RC: 0, Stdout: "Connected.\n"},
		"kopia repository throttle get": {RC: 1, Stderr: "boom"},
	})
	res, err := moduleKopiaRepositoryInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when repository throttle get fails")
	}
}

func TestModuleKopiaRepositoryInfoNoConfig(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kopia repository status":       {RC: 0, Stdout: ""},
		"kopia repository throttle get": {RC: 0, Stdout: ""},
	})
	res, err := moduleKopiaRepositoryInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if _, ok := res.Extra["repository_status"]; ok {
		t.Fatal("want repository_status omitted when the command's own stdout is empty")
	}
}
