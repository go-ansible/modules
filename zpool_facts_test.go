package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleZpoolFactsNamed(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zpool list -H -o name rpool 2>/dev/null":       {RC: 0},
		"zpool get -H -o name,property,value all rpool": {RC: 0, Stdout: "rpool\tsize\t49.8G\nrpool\thealth\tONLINE\n"},
	})
	res, err := moduleZpoolFacts(context.Background(), conn, map[string]any{"name": "rpool"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts := res.Extra["zfs_pools"].([]any)
	if len(facts) != 1 {
		t.Fatalf("facts = %v", facts)
	}
	entry := facts[0].(map[string]any)
	if entry["name"] != "rpool" || entry["size"] != "49.8G" || entry["health"] != "ONLINE" {
		t.Fatalf("entry = %v", entry)
	}
}

func TestModuleZpoolFactsAllPools(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zpool get -H -o name,property,value all": {
			RC:     0,
			Stdout: "rpool\tsize\t49.8G\ntank\tsize\t1T\n",
		},
	})
	res, err := moduleZpoolFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	facts := res.Extra["zfs_pools"].([]any)
	if len(facts) != 2 {
		t.Fatalf("facts = %v", facts)
	}
	if _, ok := res.Extra["name"]; ok {
		t.Fatal("want no name key when name was not given")
	}
}

func TestModuleZpoolFactsParsableAlias(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zpool list -H -o name tank 2>/dev/null":                {RC: 0},
		"zpool get -H -p -o name,property,value free,size tank": {RC: 0, Stdout: "tank\tfree\t1000\n"},
	})
	res, err := moduleZpoolFacts(context.Background(), conn, map[string]any{
		"pool": "tank", "parsable": true, "properties": "free,size",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["parsable"] != true {
		t.Fatalf("res.Extra = %v", res.Extra)
	}
}

func TestModuleZpoolFactsNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zpool list -H -o name nope 2>/dev/null": {RC: 1},
	})
	res, err := moduleZpoolFacts(context.Background(), conn, map[string]any{"name": "nope"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a nonexistent pool")
	}
}
