package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleZfsFactsBasic(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zfs list -H -o name rpool/myfs 2>/dev/null": {RC: 0},
		"zfs get -H -t all -o name,property,value all rpool/myfs": {
			RC: 0,
			Stdout: "rpool/myfs\tused\t4.41G\n" +
				"rpool/myfs\tmountpoint\t/rpool/myfs\n",
		},
	})
	res, err := moduleZfsFacts(context.Background(), conn, map[string]any{"name": "rpool/myfs"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, want a plain Ok", res)
	}
	facts, ok := res.Extra["zfs_datasets"].([]any)
	if !ok || len(facts) != 1 {
		t.Fatalf("zfs_datasets = %v", res.Extra["zfs_datasets"])
	}
	entry := facts[0].(map[string]any)
	if entry["name"] != "rpool/myfs" || entry["used"] != "4.41G" || entry["mountpoint"] != "/rpool/myfs" {
		t.Fatalf("entry = %v", entry)
	}
}

func TestModuleZfsFactsWithFlags(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zfs list -H -o name data/home 2>/dev/null": {RC: 0},
		"zfs get -H -p -r -d 2 -t filesystem -o name,property,value used,quota data/home": {
			RC:     0,
			Stdout: "data/home\tused\t123\n",
		},
	})
	res, err := moduleZfsFacts(context.Background(), conn, map[string]any{
		"name": "data/home", "recurse": true, "parsable": true, "depth": 2,
		"type": []string{"filesystem"}, "properties": "used,quota",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["parsable"] != true || res.Extra["recurse"] != true {
		t.Fatalf("res.Extra = %v", res.Extra)
	}
}

func TestModuleZfsFactsDoesNotExist(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zfs list -H -o name rpool/nope 2>/dev/null": {RC: 1},
	})
	res, err := moduleZfsFacts(context.Background(), conn, map[string]any{"name": "rpool/nope"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a nonexistent dataset")
	}
}

func TestModuleZfsFactsTypeAllExclusive(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleZfsFacts(context.Background(), conn, map[string]any{
		"name": "rpool/myfs", "type": []string{"all", "volume"},
	}); err == nil {
		t.Fatal("want error: type=all is mutually exclusive with other values")
	}
}

func TestModuleZfsFactsMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleZfsFacts(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleZfsFactsAlias(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zfs list -H -o name rpool/export/home 2>/dev/null":              {RC: 0},
		"zfs get -H -t all -o name,property,value all rpool/export/home": {RC: 0, Stdout: ""},
	})
	res, err := moduleZfsFacts(context.Background(), conn, map[string]any{"dataset": "rpool/export/home"})
	if err != nil {
		t.Fatal(err)
	}
	facts := res.Extra["zfs_datasets"].([]any)
	if len(facts) != 0 {
		t.Fatalf("facts = %v, want empty", facts)
	}
}
