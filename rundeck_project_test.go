package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRundeckProjectCreate(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v rd": {{RC: 0, Stdout: "/usr/bin/rd"}},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd projects info -p Project_01": {
			{RC: 1, Stderr: "not found"},
			{RC: 0, Stdout: `{"name":"Project_01"}`},
		},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd projects create -p Project_01": {{RC: 0}},
	})
	res, err := moduleRundeckProject(context.Background(), conn, map[string]any{
		"name": "Project_01", "url": "https://rundeck.example.org", "api_token": "tok",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	after, _ := res.Extra["after"].(map[string]any)
	if after["name"] != "Project_01" {
		t.Fatalf("after = %v", res.Extra["after"])
	}
}

func TestModuleRundeckProjectAlreadyExists(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v rd": {{RC: 0}},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd projects info -p Project_01": {
			{RC: 0, Stdout: `{"name":"Project_01"}`},
		},
	})
	res, err := moduleRundeckProject(context.Background(), conn, map[string]any{
		"name": "Project_01", "url": "https://rundeck.example.org", "api_token": "tok",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: project already exists")
	}
}

func TestModuleRundeckProjectRemove(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v rd": {{RC: 0}},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd projects info -p Project_01": {
			{RC: 0, Stdout: `{"name":"Project_01"}`},
		},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd projects delete -p Project_01": {{RC: 0}},
	})
	res, err := moduleRundeckProject(context.Background(), conn, map[string]any{
		"name": "Project_01", "url": "https://rundeck.example.org", "api_token": "tok", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRundeckProjectRemoveAlreadyAbsent(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v rd": {{RC: 0}},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd projects info -p Project_01": {
			{RC: 1},
		},
	})
	res, err := moduleRundeckProject(context.Background(), conn, map[string]any{
		"name": "Project_01", "url": "https://rundeck.example.org", "api_token": "tok", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleRundeckProjectValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRundeckProject(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
	if _, err := moduleRundeckProject(context.Background(), conn, map[string]any{"name": "x"}); err == nil {
		t.Fatal("want error for missing url")
	}
	if _, err := moduleRundeckProject(context.Background(), conn, map[string]any{"name": "x", "url": "u"}); err == nil {
		t.Fatal("want error for missing api_token")
	}
}

func TestModuleRundeckProjectMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v rd": {RC: 1},
	})
	res, err := moduleRundeckProject(context.Background(), conn, map[string]any{
		"name": "x", "url": "u", "api_token": "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when rd is missing")
	}
}
