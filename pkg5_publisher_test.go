package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePkg5PublisherCreateNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg publisher -Ftsv": {RC: 0, Stdout: "PUBLISHER\tTYPE\tSTATUS\tURI\tSTICKY\tSYSPUB\tENABLED\n"},
		"pkg set-publisher --remove-origin=* --add-origin=https://pkg.example.com/site/ site": {RC: 0},
	})
	res, err := modulePkg5Publisher(context.Background(), conn, map[string]any{
		"name": "site", "origin": []string{"https://pkg.example.com/site/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModulePkg5PublisherAlreadyUpToDate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg publisher -Ftsv": {RC: 0, Stdout: "PUBLISHER\tTYPE\tSTATUS\tURI\tSTICKY\tSYSPUB\tENABLED\n" +
			"site\torigin\tonline\thttps://pkg.example.com/site/\ttrue\tfalse\ttrue\n"},
	})
	res, err := modulePkg5Publisher(context.Background(), conn, map[string]any{
		"name": "site", "origin": []string{"https://pkg.example.com/site/"}, "sticky": true, "enabled": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModulePkg5PublisherStickyChange(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg publisher -Ftsv": {RC: 0, Stdout: "PUBLISHER\tTYPE\tSTATUS\tURI\tSTICKY\tSYSPUB\tENABLED\n" +
			"solaris\torigin\tonline\thttps://pkg.oracle.com/solaris/support/\tfalse\tfalse\ttrue\n"},
		"pkg set-publisher --sticky solaris": {RC: 0},
	})
	res, err := modulePkg5Publisher(context.Background(), conn, map[string]any{
		"name": "solaris", "sticky": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePkg5PublisherAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg publisher -Ftsv": {RC: 0, Stdout: "PUBLISHER\tTYPE\tSTATUS\tURI\tSTICKY\tSYSPUB\tENABLED\n" +
			"site\torigin\tonline\thttps://pkg.example.com/site/\ttrue\tfalse\ttrue\n"},
		"pkg unset-publisher site": {RC: 0},
	})
	res, err := modulePkg5Publisher(context.Background(), conn, map[string]any{
		"name": "site", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePkg5PublisherAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg publisher -Ftsv": {RC: 0, Stdout: "PUBLISHER\tTYPE\tSTATUS\tURI\tSTICKY\tSYSPUB\tENABLED\n"},
	})
	res, err := modulePkg5Publisher(context.Background(), conn, map[string]any{
		"name": "site", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePkg5PublisherMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePkg5Publisher(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
