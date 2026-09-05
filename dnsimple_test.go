package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleDnsimpleNoDomainListsAll(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dnsimple":          {RC: 0},
		"dnsimple domains list --json": {RC: 0, Stdout: `{"data":[{"id":1,"name":"example.com"}]}`},
	})
	res, err := moduleDnsimple(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleDnsimpleDomainCreatedWhenMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dnsimple":                        {RC: 0},
		"dnsimple domains get example.com --json":    {RC: 1},
		"dnsimple domains create example.com --json": {RC: 0, Stdout: `{"data":{"id":1,"name":"example.com"}}`},
	})
	res, err := moduleDnsimple(context.Background(), conn, map[string]any{"domain": "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleDnsimpleDomainAlreadyExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dnsimple":                     {RC: 0},
		"dnsimple domains get example.com --json": {RC: 0, Stdout: `{"data":{"id":1,"name":"example.com"}}`},
	})
	res, err := moduleDnsimple(context.Background(), conn, map[string]any{"domain": "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleDnsimpleDomainDeleted(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dnsimple":                       {RC: 0},
		"dnsimple domains get example.com --json":   {RC: 0, Stdout: `{"data":{"id":1}}`},
		"dnsimple domains delete example.com --yes": {RC: 0},
	})
	res, err := moduleDnsimple(context.Background(), conn, map[string]any{"domain": "example.com", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleDnsimpleRecordCreated(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dnsimple": {RC: 0},
		"dnsimple records list example.com --name test --json":                                           {RC: 0, Stdout: `{"data":[]}`},
		"dnsimple records create example.com --type A --name test --content 127.0.0.1 --ttl 3600 --json": {RC: 0, Stdout: `{"data":{"id":10}}`},
	})
	res, err := moduleDnsimple(context.Background(), conn, map[string]any{
		"domain": "example.com", "record": "test", "type": "A", "value": "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleDnsimpleRecordAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dnsimple": {RC: 0},
		"dnsimple records list example.com --name test --json": {RC: 0, Stdout: `{"data":[{"id":10,"name":"test","type":"A","content":"127.0.0.1","ttl":3600}]}`},
	})
	res, err := moduleDnsimple(context.Background(), conn, map[string]any{
		"domain": "example.com", "record": "test", "type": "A", "value": "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleDnsimpleRecordTTLUpdated(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dnsimple": {RC: 0},
		"dnsimple records list example.com --name test --json":     {RC: 0, Stdout: `{"data":[{"id":10,"name":"test","type":"A","content":"127.0.0.1","ttl":600}]}`},
		"dnsimple records update example.com 10 --ttl 3600 --json": {RC: 0, Stdout: `{"data":{"id":10,"ttl":3600}}`},
	})
	res, err := moduleDnsimple(context.Background(), conn, map[string]any{
		"domain": "example.com", "record": "test", "type": "A", "value": "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleDnsimpleRecordDeleted(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dnsimple": {RC: 0},
		"dnsimple records list example.com --name test --json": {RC: 0, Stdout: `{"data":[{"id":10,"name":"test","type":"A","content":"127.0.0.1","ttl":3600}]}`},
		"dnsimple records delete example.com 10 --yes":         {RC: 0},
	})
	res, err := moduleDnsimple(context.Background(), conn, map[string]any{
		"domain": "example.com", "record": "test", "type": "A", "value": "127.0.0.1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleDnsimpleRecordIDsMissingFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dnsimple":                      {RC: 0},
		"dnsimple records list example.com --json": {RC: 0, Stdout: `{"data":[{"id":10}]}`},
	})
	res, err := moduleDnsimple(context.Background(), conn, map[string]any{
		"domain": "example.com", "record_ids": []any{"10", "20"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleDnsimpleRecordIDsAbsentRemovesMatching(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dnsimple":                          {RC: 0},
		"dnsimple records list example.com --json":     {RC: 0, Stdout: `{"data":[{"id":10},{"id":20}]}`},
		"dnsimple records delete example.com 10 --yes": {RC: 0},
	})
	res, err := moduleDnsimple(context.Background(), conn, map[string]any{
		"domain": "example.com", "record_ids": []any{"10"}, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}
