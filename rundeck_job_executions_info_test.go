package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRundeckJobExecutionsInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v rd": {RC: 0},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd executions query --jobids xxxxxxxxxxxxxxxxx --max 20 --offset 0": {
			RC: 0, Stdout: `{"executions":[{"id":1,"status":"succeeded"}],"paging":{"count":1,"max":20,"offset":0,"total":1}}`,
		},
	})
	res, err := moduleRundeckJobExecutionsInfo(context.Background(), conn, map[string]any{
		"job_id": "xxxxxxxxxxxxxxxxx", "url": "https://rundeck.example.org", "api_token": "tok",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	execs, _ := res.Extra["executions"].([]map[string]any)
	if len(execs) != 1 || execs[0]["id"].(float64) != 1 {
		t.Fatalf("executions = %v", res.Extra["executions"])
	}
	paging, _ := res.Extra["paging"].(map[string]any)
	if paging["total"].(float64) != 1 {
		t.Fatalf("paging = %v", res.Extra["paging"])
	}
}

func TestModuleRundeckJobExecutionsInfoWithStatus(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v rd": {RC: 0},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd executions query --jobids xxxxxxxxxxxxxxxxx --max 20 --offset 0 --status failed": {
			RC: 0, Stdout: `{"executions":[],"paging":{"count":0,"max":20,"offset":0,"total":0}}`,
		},
	})
	res, err := moduleRundeckJobExecutionsInfo(context.Background(), conn, map[string]any{
		"job_id": "xxxxxxxxxxxxxxxxx", "url": "https://rundeck.example.org", "api_token": "tok",
		"status": "failed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRundeckJobExecutionsInfoValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRundeckJobExecutionsInfo(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing job_id")
	}
	if _, err := moduleRundeckJobExecutionsInfo(context.Background(), conn, map[string]any{
		"job_id": "x", "url": "u", "api_token": "t", "status": "bogus",
	}); err == nil {
		t.Fatal("want error for bad status")
	}
}
