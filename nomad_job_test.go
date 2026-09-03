package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleNomadJobMissingHost(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleNomadJob(context.Background(), conn, map[string]any{
		"state": "present", "content": "job {}",
	}); err == nil {
		t.Fatal("want error for missing host")
	}
}

func TestModuleNomadJobRunContent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nomad job run -detach -address=https://localhost:4646": {RC: 0, Stdout: "==> ..."},
	})
	res, err := moduleNomadJob(context.Background(), conn, map[string]any{
		"host": "localhost", "state": "present", "content": "job \"example\" {}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Stdins) == 0 || conn.Stdins[0] != "job \"example\" {}" {
		t.Fatalf("stdins = %v", conn.Stdins)
	}
}

func TestModuleNomadJobStopAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nomad job inspect api -json -address=https://localhost:4646": {RC: 0, Stdout: `{"Job":{"ID":"api","Stop":false}}`},
		"nomad job stop api -address=https://localhost:4646":          {RC: 0},
	})
	res, err := moduleNomadJob(context.Background(), conn, map[string]any{
		"host": "localhost", "state": "absent", "name": "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleNomadJobStopAlreadyStopped(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nomad job inspect api -json -address=https://localhost:4646": {RC: 0, Stdout: `{"Job":{"ID":"api","Stop":true}}`},
	})
	res, err := moduleNomadJob(context.Background(), conn, map[string]any{
		"host": "localhost", "state": "absent", "name": "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleNomadJobStopNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nomad job inspect api -json -address=https://localhost:4646": {RC: 1, Stderr: "job not found"},
	})
	res, err := moduleNomadJob(context.Background(), conn, map[string]any{
		"host": "localhost", "state": "absent", "name": "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: job never existed")
	}
}

func TestModuleNomadJobForceStart(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nomad job inspect api -json -address=https://localhost:4646": {RC: 0, Stdout: `{"Job":{"ID":"api","Stop":true}}`},
	})
	res, err := moduleNomadJob(context.Background(), conn, map[string]any{
		"host": "localhost", "state": "present", "name": "api", "force_start": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	found := false
	for _, c := range conn.Commands {
		if strings.HasPrefix(c, "nomad job run -json -detach") {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want a job run -json call", conn.Commands)
	}
	last := conn.Stdins[len(conn.Stdins)-1]
	if !strings.Contains(last, `"Stop":false`) {
		t.Fatalf("stdin = %q, want Stop cleared", last)
	}
}

func TestModuleNomadJobPresentNameOnlyNoForce(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nomad job inspect api -json -address=https://localhost:4646": {RC: 0, Stdout: `{"Job":{"ID":"api","Stop":false}}`},
	})
	res, err := moduleNomadJob(context.Background(), conn, map[string]any{
		"host": "localhost", "state": "present", "name": "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleNomadJobInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleNomadJob(context.Background(), conn, map[string]any{
		"host": "localhost", "state": "bogus", "name": "api",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
