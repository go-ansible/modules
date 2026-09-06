package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRedfishInfoCheckAvailability(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com root; rm -f /tmp/redfishtool-cfg.json`
	conn.on[cmd] = remoteexec.Result{RC: 0, Stdout: `{"Id":"RootService","Name":"Root Service","RedfishVersion":"1.6.0","Vendor":"Contoso","UUID":"abc-123"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Service"}, "command": []any{"CheckAvailability"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	service, _ := facts["service"].(map[string]any)
	if service["available"] != true {
		t.Fatalf("service = %+v", service)
	}
	entries, _ := service["entries"].(map[string]any)
	if entries["Vendor"] != "Contoso" || entries["UUID"] != "abc-123" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestModuleRedfishInfoCheckAvailabilityUnreachableIsSoftFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com root; rm -f /tmp/redfishtool-cfg.json`
	conn.on[cmd] = remoteexec.Result{RC: 1, Stderr: "Error, could not connect"}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Service"}, "command": []any{"CheckAvailability"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want ok (unreachable is a soft failure, not a module failure)", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	service, _ := facts["service"].(map[string]any)
	if service["available"] != false {
		t.Fatalf("service = %+v, want available:false", service)
	}
}

func TestModuleRedfishInfoGetSystemInventory(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[cmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/","Model":"PowerEdge R640","SerialNumber":"ABC123","PowerState":"On","Unrelated":"ignored"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"}, "command": []any{"GetSystemInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	system, _ := facts["system"].([]any)
	if len(system) != 1 {
		t.Fatalf("system = %+v, want 1 entry", system)
	}
	pair, _ := system[0].([]any)
	if len(pair) != 2 {
		t.Fatalf("pair = %+v, want [uriMap, entriesMap]", pair)
	}
	uriMap, _ := pair[0].(map[string]any)
	if uriMap["system_uri"] != "/redfish/v1/Systems/1/" {
		t.Fatalf("uriMap = %+v", uriMap)
	}
	entries, _ := pair[1].(map[string]any)
	if entries["Model"] != "PowerEdge R640" || entries["SerialNumber"] != "ABC123" {
		t.Fatalf("entries = %+v", entries)
	}
	if _, ok := entries["Unrelated"]; ok {
		t.Fatalf("entries should not include unrelated properties: %+v", entries)
	}
}

func TestModuleRedfishInfoDefaultCommand(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[cmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/","Model":"X"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	if _, ok := facts["system"]; !ok {
		t.Fatalf("facts = %+v, want default command GetSystemInventory to populate 'system'", facts)
	}
}

func TestModuleRedfishInfoGetBootOverrideEnabled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[cmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/","Boot":{"BootSourceOverrideEnabled":"Once","BootSourceOverrideTarget":"Pxe"}}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"}, "command": []any{"GetBootOverride"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	bootOverride, _ := facts["boot_override"].([]any)
	pair, _ := bootOverride[0].([]any)
	entries, _ := pair[1].(map[string]any)
	if entries["BootSourceOverrideTarget"] != "Pxe" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestModuleRedfishInfoGetBootOverrideDisabledIsEmpty(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[cmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/","Boot":{"BootSourceOverrideEnabled":false,"BootSourceOverrideTarget":"Pxe"}}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"}, "command": []any{"GetBootOverride"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	bootOverride, _ := facts["boot_override"].([]any)
	pair, _ := bootOverride[0].([]any)
	entries, _ := pair[1].(map[string]any)
	if len(entries) != 0 {
		t.Fatalf("entries = %+v, want empty (override disabled)", entries)
	}
}

func TestModuleRedfishInfoGetPowerRestorePolicy(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[cmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/","PowerRestorePolicy":"AlwaysOn"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"}, "command": []any{"GetPowerRestorePolicy"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	prp, _ := facts["power_restore_policy"].([]any)
	pair, _ := prp[0].([]any)
	if pair[1] != "AlwaysOn" {
		t.Fatalf("power_restore_policy = %+v", pair)
	}
}

func TestModuleRedfishInfoSystemsNotFoundFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[cmd] = remoteexec.Result{RC: 1, Stderr: "Error, no such resource"}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"}, "command": []any{"GetSystemInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (Systems resource genuinely not found)", res)
	}
}

func TestModuleRedfishInfoInvalidCategoryFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Bogus"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}

func TestModuleRedfishInfoNotYetWiredCategoryFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Manager"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (Manager not wired yet this batch)", res)
	}
}

func TestModuleRedfishInfoCategoryAllNotSupportedFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"all"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (category all not supported yet)", res)
	}
}

func TestModuleRedfishInfoMultipleCategoriesMerge(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	rootCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com root; rm -f /tmp/redfishtool-cfg.json`
	conn.on[rootCmd] = remoteexec.Result{RC: 0, Stdout: `{"Vendor":"Contoso"}`}
	sysCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/","Model":"X"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Service", "Systems"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	if _, ok := facts["service"]; !ok {
		t.Fatalf("facts missing service: %+v", facts)
	}
	if _, ok := facts["system"]; !ok {
		t.Fatalf("facts missing system: %+v", facts)
	}
}

func TestModuleRedfishInfoMissingBaseuriFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishInfo(context.Background(), conn, map[string]any{
		"category": []any{"Systems"}, "username": "admin", "password": "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}
