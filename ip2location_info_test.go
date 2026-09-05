package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIp2locationInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ip2locationio": {RC: 0},
		"ip2locationio -o json":    {RC: 0, Stdout: `{"ip":"8.8.8.8","country_code":"US","country_name":"United States of America","region_name":"California","city_name":"Mountain View","latitude":37.3860,"longitude":-122.0838,"zip_code":"94035","time_zone":"-08:00","asn":"15169","as":"Google LLC","is_proxy":false}`},
	})
	res, err := moduleIp2locationInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
	if res.Extra["ip"] != "8.8.8.8" {
		t.Fatalf("ip = %v", res.Extra["ip"])
	}
	if res.Extra["country_code"] != "US" {
		t.Fatalf("country_code = %v", res.Extra["country_code"])
	}
}

func TestModuleIp2locationInfoExplicitIP(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ip2locationio":      {RC: 0},
		"ip2locationio -o json 1.2.3.4": {RC: 0, Stdout: `{"ip":"1.2.3.4","country_code":"AU"}`},
	})
	res, err := moduleIp2locationInfo(context.Background(), conn, map[string]any{"ip": "1.2.3.4"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["country_code"] != "AU" {
		t.Fatalf("country_code = %v", res.Extra["country_code"])
	}
}

func TestModuleIp2locationInfoMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ip2locationio": {RC: 1},
	})
	res, err := moduleIp2locationInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleIp2locationInfoNonZeroExit(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ip2locationio": {RC: 0},
		"ip2locationio -o json":    {RC: 1, Stderr: "network error"},
	})
	res, err := moduleIp2locationInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}
