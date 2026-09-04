package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRundeckJobRunWaitSuccess(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v rd": {{RC: 0}},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd run --id xxxxxxxxxxxxxxxxx --loglevel INFO": {
			{RC: 0, Stdout: `{"id":1,"status":"running"}`},
		},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd executions info -e 1": {
			{RC: 0, Stdout: `{"id":1,"status":"succeeded","output":"Test!"}`},
		},
	})
	res, err := moduleRundeckJobRun(context.Background(), conn, map[string]any{
		"job_id": "xxxxxxxxxxxxxxxxx", "url": "https://rundeck.example.org", "api_token": "tok",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	info, _ := res.Extra["execution_info"].(map[string]any)
	if info["status"] != "succeeded" {
		t.Fatalf("execution_info = %v", res.Extra["execution_info"])
	}
}

func TestModuleRundeckJobRunFailed(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v rd": {{RC: 0}},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd run --id xxxxxxxxxxxxxxxxx --loglevel INFO": {
			{RC: 0, Stdout: `{"id":1,"status":"running"}`},
		},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd executions info -e 1": {
			{RC: 0, Stdout: `{"id":1,"status":"failed"}`},
		},
	})
	res, err := moduleRundeckJobRun(context.Background(), conn, map[string]any{
		"job_id": "xxxxxxxxxxxxxxxxx", "url": "https://rundeck.example.org", "api_token": "tok",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a failed job execution")
	}
}

func TestModuleRundeckJobRunFireAndForget(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v rd": {{RC: 0}},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd run --id xxxxxxxxxxxxxxxxx --loglevel INFO": {
			{RC: 0, Stdout: `{"id":1,"status":"running"}`},
		},
	})
	res, err := moduleRundeckJobRun(context.Background(), conn, map[string]any{
		"job_id": "xxxxxxxxxxxxxxxxx", "url": "https://rundeck.example.org", "api_token": "tok",
		"wait_execution": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	for _, c := range conn.Commands {
		if c == "RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd executions info -e 1" {
			t.Fatal("must not poll when wait_execution is false")
		}
	}
}

func TestModuleRundeckJobRunOptionsAndFilter(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v rd": {{RC: 0}},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd run --id xxxxxxxxxxxxxxxxx --loglevel DEBUG --filter tags:web -- -option_1 value_1": {
			{RC: 0, Stdout: `{"id":1,"status":"running"}`},
		},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd executions info -e 1": {
			{RC: 0, Stdout: `{"id":1,"status":"succeeded"}`},
		},
	})
	res, err := moduleRundeckJobRun(context.Background(), conn, map[string]any{
		"job_id": "xxxxxxxxxxxxxxxxx", "url": "https://rundeck.example.org", "api_token": "tok",
		"loglevel": "debug", "filter_nodes": "tags:web",
		"job_options": map[string]any{"option_1": "value_1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRundeckJobRunNonStringOption(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleRundeckJobRun(context.Background(), conn, map[string]any{
		"job_id": "x", "url": "u", "api_token": "t",
		"job_options": map[string]any{"exit_code": 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v, want a plain Ok (matching real rundeck_job_run's own exit_json here)", res)
	}
	if res.Msg == "" {
		t.Fatal("want a message explaining the non-string option")
	}
}

func TestModuleRundeckJobRunAbsoluteScheduleGap(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleRundeckJobRun(context.Background(), conn, map[string]any{
		"job_id": "x", "url": "u", "api_token": "t",
		"run_at_time": "2021-10-05T15:45:00-03:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: run_at_time has no rd run equivalent this port could verify")
	}
}

func TestModuleRundeckJobRunTimeoutAbort(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v rd": {RC: 0},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd run --id xxxxxxxxxxxxxxxxx --loglevel INFO": {
			RC: 0, Stdout: `{"id":1,"status":"running"}`,
		},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd executions info -e 1": {
			RC: 0, Stdout: `{"id":1,"status":"running"}`,
		},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd executions kill -e 1": {RC: 0},
	})
	res, err := moduleRundeckJobRun(context.Background(), conn, map[string]any{
		"job_id": "xxxxxxxxxxxxxxxxx", "url": "https://rundeck.example.org", "api_token": "tok",
		"wait_execution_timeout": 0, "wait_execution_delay": 0, "abort_on_timeout": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed on timeout")
	}
	found := false
	for _, c := range conn.Commands {
		if c == "RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd executions kill -e 1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want an abort (kill) call", conn.Commands)
	}
}

func TestModuleRundeckJobRunValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRundeckJobRun(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing job_id")
	}
	if _, err := moduleRundeckJobRun(context.Background(), conn, map[string]any{
		"job_id": "x", "loglevel": "bogus",
	}); err == nil {
		t.Fatal("want error for bad loglevel")
	}
}
