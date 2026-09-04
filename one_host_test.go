package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const oneEmptyHostPool = `<HOST_POOL></HOST_POOL>`

func oneHostPoolXML(id, state, clusterID int, template string) string {
	return `<HOST_POOL><HOST><ID>` + fmtAny(id) + `</ID><NAME>host1</NAME><STATE>` + fmtAny(state) +
		`</STATE><CLUSTER_ID>` + fmtAny(clusterID) + `</CLUSTER_ID><TEMPLATE>` + template + `</TEMPLATE></HOST></HOST_POOL>`
}

func TestModuleOneHostCreate(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v onehost": {{RC: 0}},
		"onehost list -x": {
			{RC: 0, Stdout: oneEmptyHostPool},
			{RC: 0, Stdout: oneHostPoolXML(5, 2, 0, "")},
		},
		"onehost create host1 -i kvm -v kvm --cluster 0": {{RC: 0}},
	})
	res, err := moduleOneHost(context.Background(), conn, map[string]any{"name": "host1"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleOneHostEnableDisabled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v onehost": {RC: 0},
		"onehost list -x":    {RC: 0, Stdout: oneHostPoolXML(7, oneHostStateDisabled, 0, "")},
		"onehost enable 7":   {RC: 0},
	})
	res, err := moduleOneHost(context.Background(), conn, map[string]any{"name": "host1", "state": "enabled"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOneHostEnableAlreadyMonitored(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v onehost": {RC: 0},
		"onehost list -x":    {RC: 0, Stdout: oneHostPoolXML(7, oneHostStateMonitored, 0, "")},
	})
	res, err := moduleOneHost(context.Background(), conn, map[string]any{"name": "host1", "state": "enabled"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: already monitored")
	}
}

func TestModuleOneHostTemplateUpdate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v onehost":          {RC: 0},
		"onehost list -x":             {RC: 0, Stdout: oneHostPoolXML(9, oneHostStateMonitored, 0, "")},
		"onehost update 9 - --append": {RC: 0},
	})
	res, err := moduleOneHost(context.Background(), conn, map[string]any{
		"name": "host1", "labels": []any{"gold", "ssd"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Stdins) == 0 || conn.Stdins[len(conn.Stdins)-1] != "LABELS = \"gold, ssd\"\n" {
		t.Fatalf("stdins = %v", conn.Stdins)
	}
}

func TestModuleOneHostClusterMove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v onehost":      {RC: 0},
		"command -v onecluster":   {RC: 0},
		"onehost list -x":         {RC: 0, Stdout: oneHostPoolXML(11, oneHostStateMonitored, 0, "")},
		"onecluster addhost 1 11": {RC: 0},
	})
	res, err := moduleOneHost(context.Background(), conn, map[string]any{
		"name": "host1", "cluster_id": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOneHostAbsentAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v onehost": {RC: 0},
		"onehost list -x":    {RC: 0, Stdout: oneEmptyHostPool},
	})
	res, err := moduleOneHost(context.Background(), conn, map[string]any{
		"name": "host1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleOneHostDisabledOnAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v onehost": {RC: 0},
		"onehost list -x":    {RC: 0, Stdout: oneEmptyHostPool},
	})
	res, err := moduleOneHost(context.Background(), conn, map[string]any{
		"name": "host1", "state": "disabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: absent host cannot be put in disabled state")
	}
}

func TestModuleOneHostValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleOneHost(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
	if _, err := moduleOneHost(context.Background(), conn, map[string]any{"name": "h", "state": "bogus"}); err == nil {
		t.Fatal("want error for bad state")
	}
	if _, err := moduleOneHost(context.Background(), conn, map[string]any{
		"name": "h", "cluster_id": 1, "cluster_name": "default",
	}); err == nil {
		t.Fatal("want error: cluster_id/cluster_name mutually exclusive")
	}
}

func TestModuleOneHostMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v onehost": {RC: 1},
	})
	res, err := moduleOneHost(context.Background(), conn, map[string]any{"name": "h"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when onehost is missing")
	}
}
