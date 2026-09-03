package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const seportListSample = `http_port_t                    tcp      80, 443, 8008, 8009
memcache_port_t                tcp      10000-10100, 10112
ssh_port_t                     tcp      22
`

func TestModuleSeportAddNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage port -l": {RC: 0, Stdout: seportListSample},
		"semanage port -a -p tcp -t http_port_t 8888": {RC: 0},
	})
	res, err := moduleSeport(context.Background(), conn, map[string]any{
		"ports": "8888", "proto": "tcp", "setype": "http_port_t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSeportAlreadyCoveredExact(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage port -l": {RC: 0, Stdout: seportListSample},
	})
	res, err := moduleSeport(context.Background(), conn, map[string]any{
		"ports": "22", "proto": "tcp", "setype": "ssh_port_t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSeportAlreadyCoveredByRange(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage port -l": {RC: 0, Stdout: seportListSample},
	})
	res, err := moduleSeport(context.Background(), conn, map[string]any{
		"ports": []any{"10005"}, "proto": "tcp", "setype": "memcache_port_t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: 10005 is covered by existing 10000-10100 range")
	}
}

func TestModuleSeportModifyExistingDifferentType(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage port -l":                           {RC: 0, Stdout: seportListSample},
		"semanage port -m -p tcp -t ssh_port_t 8009": {RC: 0},
	})
	res, err := moduleSeport(context.Background(), conn, map[string]any{
		"ports": "8009", "proto": "tcp", "setype": "ssh_port_t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: 8009 already has a type (http_port_t), so -m not -a")
	}
}

func TestModuleSeportCommaSeparatedAndRange(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage port -l": {RC: 0, Stdout: seportListSample},
		"semanage port -a -p tcp -t memcache_port_t 10112-10120": {RC: 0},
	})
	res, err := moduleSeport(context.Background(), conn, map[string]any{
		"ports": "10112-10120", "proto": "tcp", "setype": "memcache_port_t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: range extends past the existing 10112 single port")
	}
}

func TestModuleSeportDeleteExact(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage port -l":           {RC: 0, Stdout: seportListSample},
		"semanage port -d -p tcp 22": {RC: 0},
	})
	res, err := moduleSeport(context.Background(), conn, map[string]any{
		"ports": "22", "proto": "tcp", "setype": "ssh_port_t", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSeportDeleteSubRangeNotRemoved(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage port -l": {RC: 0, Stdout: seportListSample},
	})
	res, err := moduleSeport(context.Background(), conn, map[string]any{
		"ports": "10005", "proto": "tcp", "setype": "memcache_port_t", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: absent only removes an EXACT existing entry, not a covered sub-range")
	}
}

func TestModuleSeportLocalFlag(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage port -C -l":                         {RC: 0, Stdout: ""},
		"semanage port -a -p tcp -t http_port_t 8888": {RC: 0},
	})
	res, err := moduleSeport(context.Background(), conn, map[string]any{
		"ports": "8888", "proto": "tcp", "setype": "http_port_t", "local": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSeportValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSeport(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing ports")
	}
	if _, err := moduleSeport(context.Background(), conn, map[string]any{"ports": "80"}); err == nil {
		t.Fatal("want error for missing proto")
	}
	if _, err := moduleSeport(context.Background(), conn, map[string]any{"ports": "80", "proto": "bogus", "setype": "x"}); err == nil {
		t.Fatal("want error for bad proto")
	}
	if _, err := moduleSeport(context.Background(), conn, map[string]any{"ports": "80", "proto": "tcp"}); err == nil {
		t.Fatal("want error for missing setype")
	}
}
