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
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
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
	systemResult, _ := facts["system"].(map[string]any)
	if systemResult["ret"] != true {
		t.Fatalf("system = %+v", systemResult)
	}
	system, _ := systemResult["entries"].([]any)
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
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
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
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
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
	bootResult, _ := facts["boot_override"].(map[string]any)
	if bootResult["ret"] != true {
		t.Fatalf("boot_override = %+v", bootResult)
	}
	bootOverride, _ := bootResult["entries"].([]any)
	pair, _ := bootOverride[0].([]any)
	entries, _ := pair[1].(map[string]any)
	if entries["BootSourceOverrideTarget"] != "Pxe" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestModuleRedfishInfoGetBootOverrideDisabledIsEmpty(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
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
	bootResult, _ := facts["boot_override"].(map[string]any)
	if bootResult["ret"] != true {
		t.Fatalf("boot_override = %+v, want ret:true (explicit false is a real success with empty entries, not a failure)", bootResult)
	}
	bootOverride, _ := bootResult["entries"].([]any)
	pair, _ := bootOverride[0].([]any)
	entries, _ := pair[1].(map[string]any)
	if len(entries) != 0 {
		t.Fatalf("entries = %+v, want empty (override disabled)", entries)
	}
}

func TestModuleRedfishInfoGetBootOverrideNoBootKeySoftFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[cmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"}, "command": []any{"GetBootOverride"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want ok (a per-command problem is a soft embed, not a module failure)", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	bootResult, _ := facts["boot_override"].(map[string]any)
	// Real get_multi_boot_override's own aggregate() wrapper discards the
	// inner get_boot_override's own "msg" entirely (it only reads "ret"
	// and "entries") — so a missing "Boot" key or a missing
	// "BootSourceOverrideEnabled" key both surface here as a bare
	// ret:false with an empty entries list, no message at all.
	if bootResult["ret"] != false {
		t.Fatalf("boot_override = %+v, want ret:false", bootResult)
	}
	entries, _ := bootResult["entries"].([]any)
	if len(entries) != 0 {
		t.Fatalf("boot_override = %+v, want empty entries (real aggregate() drops a failed member entirely)", bootResult)
	}
}

func TestModuleRedfishInfoGetBootOverrideNoEnabledKeySoftFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[cmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/","Boot":{"BootSourceOverrideTarget":"Pxe"}}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"}, "command": []any{"GetBootOverride"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want ok (a per-command problem is a soft embed, not a module failure)", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	bootResult, _ := facts["boot_override"].(map[string]any)
	if bootResult["ret"] != false {
		t.Fatalf("boot_override = %+v, want ret:false (BootSourceOverrideEnabled missing entirely)", bootResult)
	}
}

func TestModuleRedfishInfoGetPowerRestorePolicy(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
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
	prpResult, _ := facts["power_restore_policy"].(map[string]any)
	if prpResult["ret"] != true {
		t.Fatalf("power_restore_policy = %+v", prpResult)
	}
	prp, _ := prpResult["entries"].([]any)
	pair, _ := prp[0].([]any)
	if pair[1] != "AlwaysOn" {
		t.Fatalf("power_restore_policy = %+v", pair)
	}
}

func TestModuleRedfishInfoSystemsNotFoundFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
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

func TestModuleRedfishInfoNotYetWiredCommandFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Update"}, "command": []any{"GetUpdateStatus"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (GetUpdateStatus not wired this batch — no way to recover the real HTTP status code)", res)
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
	sysCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
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

func TestModuleRedfishInfoGetChassisInventory(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Chassis; rm -f /tmp/redfishtool-cfg.json`
	conn.on[cmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Chassis/1/","Name":"Computer System Chassis","Id":"1","ChassisType":"RackMount","Model":"PowerEdge R640","SerialNumber":"XYZ789","Unrelated":"ignored"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Chassis"}, "command": []any{"GetChassisInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	chassis, _ := facts["chassis"].(map[string]any)
	if chassis["ret"] != true {
		t.Fatalf("chassis = %+v", chassis)
	}
	entries, _ := chassis["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1", entries)
	}
	entry, _ := entries[0].(map[string]any)
	if entry["Model"] != "PowerEdge R640" || entry["SerialNumber"] != "XYZ789" {
		t.Fatalf("entry = %+v", entry)
	}
	if _, ok := entry["Unrelated"]; ok {
		t.Fatalf("entry should not include unrelated properties: %+v", entry)
	}
}

func TestModuleRedfishInfoGetFanInventory(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	chassisCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Chassis; rm -f /tmp/redfishtool-cfg.json`
	conn.on[chassisCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Chassis/1/","Thermal":{"@odata.id":"/redfish/v1/Chassis/1/Thermal"}}`}
	thermalCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Chassis/1/Thermal; rm -f /tmp/redfishtool-cfg.json`
	conn.on[thermalCmd] = remoteexec.Result{RC: 0, Stdout: `{"Fans":[{"Name":"Fan1","FanName":"Fan 1","Reading":5000,"ReadingUnits":"RPM","Status":{"Health":"OK"},"Unrelated":"x"}]}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Chassis"}, "command": []any{"GetFanInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	fan, _ := facts["fan"].(map[string]any)
	if fan["ret"] != true {
		t.Fatalf("fan = %+v", fan)
	}
	entries, _ := fan["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1", entries)
	}
	entry, _ := entries[0].(map[string]any)
	if entry["FanName"] != "Fan 1" || entry["Reading"] != float64(5000) {
		t.Fatalf("entry = %+v", entry)
	}
	if _, ok := entry["Unrelated"]; ok {
		t.Fatalf("entry should not include unrelated properties: %+v", entry)
	}
}

func TestModuleRedfishInfoGetFanInventoryNoThermalIsEmptyNotFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	chassisCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Chassis; rm -f /tmp/redfishtool-cfg.json`
	conn.on[chassisCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Chassis/1/"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Chassis"}, "command": []any{"GetFanInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want ok (no Thermal link is real Ansible's own silent skip, not a failure)", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	fan, _ := facts["fan"].(map[string]any)
	entries, _ := fan["entries"].([]any)
	if len(entries) != 0 {
		t.Fatalf("fan = %+v, want empty entries", fan)
	}
}

func TestModuleRedfishInfoGetFanInventoryThermalWithoutFansSoftFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	chassisCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Chassis; rm -f /tmp/redfishtool-cfg.json`
	conn.on[chassisCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Chassis/1/","Thermal":{"@odata.id":"/redfish/v1/Chassis/1/Thermal"}}`}
	thermalCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Chassis/1/Thermal; rm -f /tmp/redfishtool-cfg.json`
	conn.on[thermalCmd] = remoteexec.Result{RC: 0, Stdout: `{"Temperatures":[]}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Chassis"}, "command": []any{"GetFanInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want ok (a per-command problem is a soft embed, not a module failure)", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	fan, _ := facts["fan"].(map[string]any)
	if fan["ret"] != false || fan["msg"] != "No Fans present" {
		t.Fatalf("fan = %+v, want soft ret:false msg:\"No Fans present\"", fan)
	}
}

func TestModuleRedfishInfoGetChassisPower(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	chassisCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Chassis; rm -f /tmp/redfishtool-cfg.json`
	conn.on[chassisCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Chassis/1/","Power":{"@odata.id":"/redfish/v1/Chassis/1/Power"}}`}
	powerCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Chassis/1/Power; rm -f /tmp/redfishtool-cfg.json`
	conn.on[powerCmd] = remoteexec.Result{RC: 0, Stdout: `{"PowerControl":[{"Name":"System Power Control","PowerConsumedWatts":450,"Status":{"Health":"OK"},"Unrelated":"x"}]}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Chassis"}, "command": []any{"GetChassisPower"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	power, _ := facts["chassis_power"].(map[string]any)
	if power["ret"] != true {
		t.Fatalf("power = %+v", power)
	}
	entries, _ := power["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1", entries)
	}
	entry, _ := entries[0].(map[string]any)
	if entry["PowerConsumedWatts"] != float64(450) {
		t.Fatalf("entry = %+v", entry)
	}
	if _, ok := entry["Unrelated"]; ok {
		t.Fatalf("entry should not include unrelated properties: %+v", entry)
	}
}

func TestModuleRedfishInfoGetChassisPowerNoPowerLinkSoftFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	chassisCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Chassis; rm -f /tmp/redfishtool-cfg.json`
	conn.on[chassisCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Chassis/1/"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Chassis"}, "command": []any{"GetChassisPower"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want ok (a per-command problem is a soft embed, not a module failure)", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	power, _ := facts["chassis_power"].(map[string]any)
	if power["ret"] != false || power["msg"] != "Power information not found." {
		t.Fatalf("power = %+v, want soft ret:false msg:\"Power information not found.\"", power)
	}
}

func TestModuleRedfishInfoChassisResourceMissingHardFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	chassisCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Chassis; rm -f /tmp/redfishtool-cfg.json`
	conn.on[chassisCmd] = remoteexec.Result{RC: 1, Stderr: "Error, could not connect"}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Chassis"}, "command": []any{"GetChassisInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (no Chassis resource at all is a category-level hard fail)", res)
	}
}

func TestModuleRedfishInfoListUsers(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	acctCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com AccountService; rm -f /tmp/redfishtool-cfg.json`
	conn.on[acctCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/AccountService/","Id":"AccountService"}`}
	listCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com AccountService Accounts list; rm -f /tmp/redfishtool-cfg.json`
	conn.on[listCmd] = remoteexec.Result{RC: 0, Stdout: `{"Members":[{"Id":"1","@odata.id":"/redfish/v1/AccountService/Accounts/1"},{"Id":"2","@odata.id":"/redfish/v1/AccountService/Accounts/2"}]}`}
	user1Cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/AccountService/Accounts/1; rm -f /tmp/redfishtool-cfg.json`
	conn.on[user1Cmd] = remoteexec.Result{RC: 0, Stdout: `{"Id":"1","UserName":"admin","RoleId":"Administrator","Enabled":true,"Locked":false,"Unrelated":"x"}`}
	user2Cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/AccountService/Accounts/2; rm -f /tmp/redfishtool-cfg.json`
	conn.on[user2Cmd] = remoteexec.Result{RC: 0, Stdout: `{"Id":"2","UserName":"","Enabled":false}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Accounts"}, "command": []any{"ListUsers"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	user, _ := facts["user"].(map[string]any)
	if user["ret"] != true {
		t.Fatalf("user = %+v", user)
	}
	entries, _ := user["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1 (empty account slot #2 filtered out)", entries)
	}
	entry, _ := entries[0].(map[string]any)
	if entry["UserName"] != "admin" || entry["RoleId"] != "Administrator" {
		t.Fatalf("entry = %+v", entry)
	}
	if _, ok := entry["Unrelated"]; ok {
		t.Fatalf("entry should not include unrelated properties: %+v", entry)
	}
}

func TestModuleRedfishInfoGetAccountServiceConfig(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	acctCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com AccountService; rm -f /tmp/redfishtool-cfg.json`
	conn.on[acctCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/AccountService/","Id":"AccountService","AccountLockoutThreshold":3}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Accounts"}, "command": []any{"GetAccountServiceConfig"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	cfg, _ := facts["accountservice_config"].(map[string]any)
	if cfg["ret"] != true {
		t.Fatalf("cfg = %+v", cfg)
	}
	entries, _ := cfg["entries"].(map[string]any)
	if entries["AccountLockoutThreshold"] != float64(3) {
		t.Fatalf("entries = %+v, want the entire raw AccountService JSON verbatim", entries)
	}
}

func TestModuleRedfishInfoAccountsResourceMissingHardFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	acctCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com AccountService; rm -f /tmp/redfishtool-cfg.json`
	conn.on[acctCmd] = remoteexec.Result{RC: 1, Stderr: "Error, could not connect"}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Accounts"}, "command": []any{"ListUsers"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (no AccountService resource at all is a category-level hard fail)", res)
	}
}

func TestModuleRedfishInfoGetSessions(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	listCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com SessionService Sessions list; rm -f /tmp/redfishtool-cfg.json`
	conn.on[listCmd] = remoteexec.Result{RC: 0, Stdout: `{"Members":[{"Id":"abc","@odata.id":"/redfish/v1/SessionService/Sessions/abc"}]}`}
	sessCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/SessionService/Sessions/abc; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sessCmd] = remoteexec.Result{RC: 0, Stdout: `{"Id":"abc","Name":"User Session","UserName":"admin","Unrelated":"x"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Sessions"}, "command": []any{"GetSessions"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	session, _ := facts["session"].(map[string]any)
	if session["ret"] != true {
		t.Fatalf("session = %+v", session)
	}
	entries, _ := session["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1", entries)
	}
	entry, _ := entries[0].(map[string]any)
	if entry["UserName"] != "admin" || entry["Name"] != "User Session" {
		t.Fatalf("entry = %+v", entry)
	}
	if _, ok := entry["Unrelated"]; ok {
		t.Fatalf("entry should not include unrelated properties: %+v", entry)
	}
}

func TestModuleRedfishInfoGetSessionsMissingHardFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	listCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com SessionService Sessions list; rm -f /tmp/redfishtool-cfg.json`
	conn.on[listCmd] = remoteexec.Result{RC: 1, Stderr: "Error, could not connect"}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Sessions"}, "command": []any{"GetSessions"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (no SessionService/Sessions resource at all is this category's own hard fail)", res)
	}
}

func TestModuleRedfishInfoGetFirmwareInventory(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	rootCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com root; rm -f /tmp/redfishtool-cfg.json`
	conn.on[rootCmd] = remoteexec.Result{RC: 0, Stdout: `{"UpdateService":{"@odata.id":"/redfish/v1/UpdateService"}}`}
	svcCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/UpdateService; rm -f /tmp/redfishtool-cfg.json`
	conn.on[svcCmd] = remoteexec.Result{RC: 0, Stdout: `{"FirmwareInventory":{"@odata.id":"/redfish/v1/UpdateService/FirmwareInventory"}}`}
	listCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/UpdateService/FirmwareInventory; rm -f /tmp/redfishtool-cfg.json`
	conn.on[listCmd] = remoteexec.Result{RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/UpdateService/FirmwareInventory/BMC"}]}`}
	memberCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/UpdateService/FirmwareInventory/BMC; rm -f /tmp/redfishtool-cfg.json`
	conn.on[memberCmd] = remoteexec.Result{RC: 0, Stdout: `{"Name":"BMC Firmware","Id":"BMC","Version":"1.2.3","Updateable":true,"Unrelated":"x"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Update"}, "command": []any{"GetFirmwareInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	fw, _ := facts["firmware"].(map[string]any)
	if fw["ret"] != true {
		t.Fatalf("firmware = %+v", fw)
	}
	entries, _ := fw["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1", entries)
	}
	entry, _ := entries[0].(map[string]any)
	if entry["Version"] != "1.2.3" || entry["Name"] != "BMC Firmware" {
		t.Fatalf("entry = %+v", entry)
	}
	if _, ok := entry["Unrelated"]; ok {
		t.Fatalf("entry should not include unrelated properties: %+v", entry)
	}
}

func TestModuleRedfishInfoGetFirmwareInventoryMissingIsSoftFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	rootCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com root; rm -f /tmp/redfishtool-cfg.json`
	conn.on[rootCmd] = remoteexec.Result{RC: 0, Stdout: `{"UpdateService":{"@odata.id":"/redfish/v1/UpdateService"}}`}
	svcCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/UpdateService; rm -f /tmp/redfishtool-cfg.json`
	conn.on[svcCmd] = remoteexec.Result{RC: 0, Stdout: `{}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Update"}, "command": []any{"GetFirmwareInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want ok (a per-command problem is a soft embed, not a module failure)", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	fw, _ := facts["firmware"].(map[string]any)
	if fw["ret"] != false || fw["msg"] != "No FirmwareInventory resource found" {
		t.Fatalf("firmware = %+v", fw)
	}
}

func TestModuleRedfishInfoGetFirmwareUpdateCapabilities(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	rootCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com root; rm -f /tmp/redfishtool-cfg.json`
	conn.on[rootCmd] = remoteexec.Result{RC: 0, Stdout: `{"UpdateService":{"@odata.id":"/redfish/v1/UpdateService"}}`}
	svcCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/UpdateService; rm -f /tmp/redfishtool-cfg.json`
	conn.on[svcCmd] = remoteexec.Result{RC: 0, Stdout: `{"MultipartHttpPushUri":"/redfish/v1/UpdateService/upload","Actions":{"#UpdateService.SimpleUpdate":{"title":"Simple Update","TransferProtocol@Redfish.AllowableValues":["HTTP","HTTPS"]}}}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Update"}, "command": []any{"GetFirmwareUpdateCapabilities"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	caps, _ := facts["firmware_update_capabilities"].(map[string]any)
	if caps["ret"] != true || caps["multipart_supported"] != true {
		t.Fatalf("caps = %+v", caps)
	}
	entries, _ := caps["entries"].(map[string]any)
	values, _ := entries["Simple Update"].([]any)
	if len(values) != 2 || values[0] != "HTTP" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestModuleRedfishInfoGetFirmwareUpdateCapabilitiesNoActionsSoftFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	rootCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com root; rm -f /tmp/redfishtool-cfg.json`
	conn.on[rootCmd] = remoteexec.Result{RC: 0, Stdout: `{"UpdateService":{"@odata.id":"/redfish/v1/UpdateService"}}`}
	svcCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/UpdateService; rm -f /tmp/redfishtool-cfg.json`
	conn.on[svcCmd] = remoteexec.Result{RC: 0, Stdout: `{}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Update"}, "command": []any{"GetFirmwareUpdateCapabilities"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want ok", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	caps, _ := facts["firmware_update_capabilities"].(map[string]any)
	if caps["ret"] != false || caps["msg"] != "Key Actions not found." {
		t.Fatalf("caps = %+v", caps)
	}
}

func TestModuleRedfishInfoUpdateServiceMissingHardFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	rootCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com root; rm -f /tmp/redfishtool-cfg.json`
	conn.on[rootCmd] = remoteexec.Result{RC: 0, Stdout: `{}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Update"}, "command": []any{"GetFirmwareInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (no UpdateService resource at all is a category-level hard fail)", res)
	}
}

func TestModuleRedfishInfoGetManagerInventory(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	mgrCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Managers; rm -f /tmp/redfishtool-cfg.json`
	conn.on[mgrCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Managers/1/","Id":"1","FirmwareVersion":"2.50","ManagerType":"BMC","Unrelated":"x"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Manager"}, "command": []any{"GetManagerInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	managerResult, _ := facts["manager"].(map[string]any)
	if managerResult["ret"] != true {
		t.Fatalf("manager = %+v", managerResult)
	}
	manager, _ := managerResult["entries"].([]any)
	if len(manager) != 1 {
		t.Fatalf("manager = %+v, want 1 entry", manager)
	}
	pair, _ := manager[0].([]any)
	uriMap, _ := pair[0].(map[string]any)
	if uriMap["manager_uri"] != "/redfish/v1/Managers/1/" {
		t.Fatalf("uriMap = %+v", uriMap)
	}
	entries, _ := pair[1].(map[string]any)
	if entries["FirmwareVersion"] != "2.50" || entries["ManagerType"] != "BMC" {
		t.Fatalf("entries = %+v", entries)
	}
	if _, ok := entries["Unrelated"]; ok {
		t.Fatalf("entries should not include unrelated properties: %+v", entries)
	}
}

func TestModuleRedfishInfoGetNetworkProtocols(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	mgrCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Managers; rm -f /tmp/redfishtool-cfg.json`
	conn.on[mgrCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Managers/1/","NetworkProtocol":{"@odata.id":"/redfish/v1/Managers/1/NetworkProtocol"}}`}
	npCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1/NetworkProtocol; rm -f /tmp/redfishtool-cfg.json`
	conn.on[npCmd] = remoteexec.Result{RC: 0, Stdout: `{"SNMP":{"ProtocolEnabled":true,"Port":161},"HTTPS":{"ProtocolEnabled":true,"Port":443},"Unrelated":"x","Id":"NetworkProtocol"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Manager"}, "command": []any{"GetNetworkProtocols"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	np, _ := facts["network_protocols"].(map[string]any)
	if np["ret"] != true {
		t.Fatalf("np = %+v", np)
	}
	entries, _ := np["entries"].(map[string]any)
	if _, ok := entries["SNMP"]; !ok {
		t.Fatalf("entries = %+v, want SNMP", entries)
	}
	if _, ok := entries["HTTPS"]; !ok {
		t.Fatalf("entries = %+v, want HTTPS", entries)
	}
	if _, ok := entries["Unrelated"]; ok {
		t.Fatalf("entries should not include non-protocol properties: %+v", entries)
	}
	if _, ok := entries["Id"]; ok {
		t.Fatalf("entries should not include Id: %+v", entries)
	}
}

func TestModuleRedfishInfoGetServiceIdentification(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	mgrCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Managers; rm -f /tmp/redfishtool-cfg.json`
	conn.on[mgrCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Managers/1/","Id":"1"}`}
	svcIDCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1; rm -f /tmp/redfishtool-cfg.json`
	conn.on[svcIDCmd] = remoteexec.Result{RC: 0, Stdout: `{"ServiceIdentification":"ABC123"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Manager"}, "command": []any{"GetServiceIdentification"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	svcID, _ := facts["service_id"].(map[string]any)
	if svcID["ret"] != true || svcID["service_identification"] != "ABC123" {
		t.Fatalf("svcID = %+v", svcID)
	}
}

func TestModuleRedfishInfoGetServiceIdentificationMissingHardFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	mgrCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Managers; rm -f /tmp/redfishtool-cfg.json`
	conn.on[mgrCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Managers/1/","Id":"1"}`}
	svcIDCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1; rm -f /tmp/redfishtool-cfg.json`
	conn.on[svcIDCmd] = remoteexec.Result{RC: 0, Stdout: `{}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Manager"}, "command": []any{"GetServiceIdentification"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (real get_service_identification calls fail_json directly, a real exception to this module's own soft-fail convention)", res)
	}
}

func TestModuleRedfishInfoGetManagerNicInventory(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	mgrCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Managers; rm -f /tmp/redfishtool-cfg.json`
	mgrJSON := `{"@odata.id":"/redfish/v1/Managers/1/","EthernetInterfaces":{"@odata.id":"/redfish/v1/Managers/1/EthernetInterfaces"}}`
	conn.on[mgrCmd] = remoteexec.Result{RC: 0, Stdout: mgrJSON}
	// redfishGetNicInventory does its own independent raw GET of the
	// manager's own URI (matching real get_nic_inventory's own
	// independent get_request), a second mock beyond the category-level
	// bare fetch.
	mgrRawCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[mgrRawCmd] = remoteexec.Result{RC: 0, Stdout: mgrJSON}
	listCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1/EthernetInterfaces; rm -f /tmp/redfishtool-cfg.json`
	conn.on[listCmd] = remoteexec.Result{RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Managers/1/EthernetInterfaces/eth0"}]}`}
	nicCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1/EthernetInterfaces/eth0; rm -f /tmp/redfishtool-cfg.json`
	conn.on[nicCmd] = remoteexec.Result{RC: 0, Stdout: `{"Name":"Manager Ethernet Interface","MACAddress":"aa:bb:cc:dd:ee:ff","Unrelated":"x"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Manager"}, "command": []any{"GetManagerNicInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	nicsResult, _ := facts["manager_nics"].(map[string]any)
	if nicsResult["ret"] != true {
		t.Fatalf("manager_nics = %+v", nicsResult)
	}
	nics, _ := nicsResult["entries"].([]any)
	if len(nics) != 1 {
		t.Fatalf("nics = %+v, want 1 entry", nics)
	}
	pair, _ := nics[0].([]any)
	uriMap, _ := pair[0].(map[string]any)
	if uriMap["resource_uri"] != "/redfish/v1/Managers/1/" {
		t.Fatalf("uriMap = %+v", uriMap)
	}
	entries, _ := pair[1].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1 nic", entries)
	}
	nic, _ := entries[0].(map[string]any)
	if nic["MACAddress"] != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("nic = %+v", nic)
	}
	if _, ok := nic["Unrelated"]; ok {
		t.Fatalf("nic should not include unrelated properties: %+v", nic)
	}
}

func TestModuleRedfishInfoManagerResourceMissingHardFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	mgrCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Managers; rm -f /tmp/redfishtool-cfg.json`
	conn.on[mgrCmd] = remoteexec.Result{RC: 1, Stderr: "Error, could not connect"}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Manager"}, "command": []any{"GetManagerInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (no Managers resource at all is a category-level hard fail)", res)
	}
}

func TestModuleRedfishInfoGetLogs(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	mgrCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Managers; rm -f /tmp/redfishtool-cfg.json`
	conn.on[mgrCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Managers/1/","LogServices":{"@odata.id":"/redfish/v1/Managers/1/LogServices"}}`}
	listCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Managers Logs list; rm -f /tmp/redfishtool-cfg.json`
	conn.on[listCmd] = remoteexec.Result{RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Managers/1/LogServices/Log1"}]}`}
	svcCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1/LogServices/Log1; rm -f /tmp/redfishtool-cfg.json`
	conn.on[svcCmd] = remoteexec.Result{RC: 0, Stdout: `{"Entries":{"@odata.id":"/redfish/v1/Managers/1/LogServices/Log1/Entries"}}`}
	entriesCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1/LogServices/Log1/Entries; rm -f /tmp/redfishtool-cfg.json`
	conn.on[entriesCmd] = remoteexec.Result{RC: 0, Stdout: `{"Description":"System Event Log","Members":[{"Severity":"Warning","Created":"2026-01-01T00:00:00Z","Message":"Fan failure","Unrelated":"x"}]}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Manager"}, "command": []any{"GetLogs"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	log, _ := facts["log"].(map[string]any)
	if log["ret"] != true {
		t.Fatalf("log = %+v", log)
	}
	entries, _ := log["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1", entries)
	}
	logGroup, _ := entries[0].(map[string]any)
	if logGroup["Description"] != "System Event Log" {
		t.Fatalf("logGroup = %+v", logGroup)
	}
	// Real get_logs derives the log name from the ENTRIES URI's own last
	// path segment, not the LogService's own Id — confirmed from its
	// own source. A real Redfish Entries collection's own @odata.id
	// conventionally ends in "/Entries" (as this fixture's does), so
	// the key really is "Entries", not "Log1" — reproduced verbatim,
	// not "fixed".
	logEntries, _ := logGroup["Entries"].([]any)
	if len(logEntries) != 1 {
		t.Fatalf("Entries = %+v, want 1", logEntries)
	}
	entry, _ := logEntries[0].(map[string]any)
	if entry["Severity"] != "Warning" || entry["Message"] != "Fan failure" {
		t.Fatalf("entry = %+v", entry)
	}
	if _, ok := entry["Unrelated"]; ok {
		t.Fatalf("entry should not include unrelated properties: %+v", entry)
	}
}

func TestModuleRedfishInfoGetLogsNoLogServicesSoftFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	mgrCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Managers; rm -f /tmp/redfishtool-cfg.json`
	conn.on[mgrCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Managers/1/"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Manager"}, "command": []any{"GetLogs"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want ok (a per-command problem is a soft embed, not a module failure)", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	log, _ := facts["log"].(map[string]any)
	if log["ret"] != false || log["msg"] != "LogServices resource not found" {
		t.Fatalf("log = %+v", log)
	}
}

func TestModuleRedfishInfoGetVirtualMedia(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	mgrCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Managers; rm -f /tmp/redfishtool-cfg.json`
	mgrJSON := `{"@odata.id":"/redfish/v1/Managers/1/","VirtualMedia":{"@odata.id":"/redfish/v1/Managers/1/VirtualMedia"}}`
	conn.on[mgrCmd] = remoteexec.Result{RC: 0, Stdout: mgrJSON}
	// redfishGetVirtualMediaInventory does its own independent raw GET
	// of the manager's own URI (matching real get_virtualmedia's own
	// independent get_request), a second mock beyond the category-level
	// bare fetch.
	mgrRawCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[mgrRawCmd] = remoteexec.Result{RC: 0, Stdout: mgrJSON}
	listCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1/VirtualMedia; rm -f /tmp/redfishtool-cfg.json`
	conn.on[listCmd] = remoteexec.Result{RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Managers/1/VirtualMedia/CD1"}]}`}
	memberCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1/VirtualMedia/CD1; rm -f /tmp/redfishtool-cfg.json`
	conn.on[memberCmd] = remoteexec.Result{RC: 0, Stdout: `{"Id":"CD1","Name":"Virtual CD","MediaTypes":["CD","DVD"],"WriteProtected":true,"Unrelated":"x"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Manager"}, "command": []any{"GetVirtualMedia"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	vmResult, _ := facts["virtual_media"].(map[string]any)
	if vmResult["ret"] != true {
		t.Fatalf("virtual_media = %+v", vmResult)
	}
	vm, _ := vmResult["entries"].([]any)
	if len(vm) != 1 {
		t.Fatalf("vm = %+v, want 1 entry", vm)
	}
	pair, _ := vm[0].([]any)
	uriMap, _ := pair[0].(map[string]any)
	if uriMap["resource_uri"] != "/redfish/v1/Managers/1/" {
		t.Fatalf("uriMap = %+v", uriMap)
	}
	entries, _ := pair[1].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1", entries)
	}
	entry, _ := entries[0].(map[string]any)
	if entry["Name"] != "Virtual CD" || entry["WriteProtected"] != true {
		t.Fatalf("entry = %+v", entry)
	}
	if _, ok := entry["Unrelated"]; ok {
		t.Fatalf("entry should not include unrelated properties: %+v", entry)
	}
}

func TestModuleRedfishInfoGetVirtualMediaNoLinkSoftFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	mgrCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Managers; rm -f /tmp/redfishtool-cfg.json`
	mgrJSON := `{"@odata.id":"/redfish/v1/Managers/1/"}`
	conn.on[mgrCmd] = remoteexec.Result{RC: 0, Stdout: mgrJSON}
	mgrRawCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[mgrRawCmd] = remoteexec.Result{RC: 0, Stdout: mgrJSON}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Manager"}, "command": []any{"GetVirtualMedia"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want ok (a per-command problem is a soft embed, not a module failure)", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	vmResult, _ := facts["virtual_media"].(map[string]any)
	// Real get_virtualmedia treats a missing "VirtualMedia" key as its
	// own soft failure ("Key VirtualMedia not found"), and
	// aggregate()'s own source discards that message entirely — the
	// final shape is a bare ret:false with empty entries, same lossy
	// pattern already confirmed for GetBootOverride/GetNicInventory.
	if vmResult["ret"] != false {
		t.Fatalf("virtual_media = %+v, want ret:false", vmResult)
	}
	entries, _ := vmResult["entries"].([]any)
	if len(entries) != 0 {
		t.Fatalf("entries = %+v, want empty", entries)
	}
}

func TestModuleRedfishInfoGetHostInterfaces(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	mgrCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Managers; rm -f /tmp/redfishtool-cfg.json`
	conn.on[mgrCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Managers/1/","HostInterfaces":{"@odata.id":"/redfish/v1/Managers/1/HostInterfaces"}}`}
	listCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1/HostInterfaces; rm -f /tmp/redfishtool-cfg.json`
	conn.on[listCmd] = remoteexec.Result{RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Managers/1/HostInterfaces/1"}]}`}
	memberCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1/HostInterfaces/1; rm -f /tmp/redfishtool-cfg.json`
	conn.on[memberCmd] = remoteexec.Result{RC: 0, Stdout: `{"Id":"1","HostInterfaceType":"NetworkHostInterface","InterfaceEnabled":true,"ManagerEthernetInterface":{"@odata.id":"/redfish/v1/Managers/1/EthernetInterfaces/usb0"},"Unrelated":"x"}`}
	nicCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1/EthernetInterfaces/usb0; rm -f /tmp/redfishtool-cfg.json`
	conn.on[nicCmd] = remoteexec.Result{RC: 0, Stdout: `{"Name":"Manager USB NIC","MACAddress":"11:22:33:44:55:66"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Manager"}, "command": []any{"GetHostInterfaces"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	hi, _ := facts["host_interfaces"].(map[string]any)
	if hi["ret"] != true {
		t.Fatalf("hi = %+v", hi)
	}
	entries, _ := hi["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1", entries)
	}
	entry, _ := entries[0].(map[string]any)
	if entry["HostInterfaceType"] != "NetworkHostInterface" {
		t.Fatalf("entry = %+v", entry)
	}
	if _, ok := entry["Unrelated"]; ok {
		t.Fatalf("entry should not include unrelated properties: %+v", entry)
	}
	mei, _ := entry["ManagerEthernetInterface"].(map[string]any)
	if mei["MACAddress"] != "11:22:33:44:55:66" {
		t.Fatalf("ManagerEthernetInterface = %+v", mei)
	}
}

func TestModuleRedfishInfoGetHostInterfacesNoneFoundSoftFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	mgrCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Managers; rm -f /tmp/redfishtool-cfg.json`
	conn.on[mgrCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Managers/1/"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Manager"}, "command": []any{"GetHostInterfaces"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want ok (a per-command problem is a soft embed, not a module failure)", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	hi, _ := facts["host_interfaces"].(map[string]any)
	if hi["ret"] != false || hi["msg"] != "No HostInterface objects found" {
		t.Fatalf("hi = %+v", hi)
	}
}

func TestModuleRedfishInfoGetSystemHealthReport(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	sysCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
	sysJSON := `{"@odata.id":"/redfish/v1/Systems/1/","Status":{"Health":"OK"},"Processors":{"@odata.id":"/redfish/v1/Systems/1/Processors"}}`
	conn.on[sysCmd] = remoteexec.Result{RC: 0, Stdout: sysJSON}
	// redfishGetHealthReport does its own independent raw GET of the
	// system's own URI (matching real get_health_report's own
	// independent get_request — it does not reuse the category-level
	// bare-fetch's own data), so the same resource needs a second mock
	// under its "raw GET <uri>" form.
	sysRawCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysRawCmd] = remoteexec.Result{RC: 0, Stdout: sysJSON}
	procCollCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/Processors; rm -f /tmp/redfishtool-cfg.json`
	conn.on[procCollCmd] = remoteexec.Result{RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Systems/1/Processors/CPU1"}]}`}
	cpuCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/Processors/CPU1; rm -f /tmp/redfishtool-cfg.json`
	conn.on[cpuCmd] = remoteexec.Result{RC: 0, Stdout: `{"Status":{"Health":"OK"}}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"}, "command": []any{"GetHealthReport"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	hrResult, _ := facts["health_report"].(map[string]any)
	if hrResult["ret"] != true {
		t.Fatalf("health_report = %+v", hrResult)
	}
	entries, _ := hrResult["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1", entries)
	}
	pair, _ := entries[0].([]any)
	uriMap, _ := pair[0].(map[string]any)
	if uriMap["system_uri"] != "/redfish/v1/Systems/1/" {
		t.Fatalf("uriMap = %+v", uriMap)
	}
	health, _ := pair[1].(map[string]any)
	system, _ := health["System"].(map[string]any)
	status, _ := system["Status"].(map[string]any)
	if status["Health"] != "OK" {
		t.Fatalf("System = %+v", system)
	}
	processors, _ := health["Processors"].([]any)
	if len(processors) != 1 {
		t.Fatalf("Processors = %+v, want 1", processors)
	}
	cpu, _ := processors[0].(map[string]any)
	if cpu["processor_uri"] != "/redfish/v1/Systems/1/Processors/CPU1" {
		t.Fatalf("cpu = %+v", cpu)
	}
	cpuStatus, _ := cpu["Status"].(map[string]any)
	if cpuStatus["Health"] != "OK" {
		t.Fatalf("cpu = %+v", cpu)
	}
	// Real get_health_report deletes any subsystem key that ends up empty
	// (Memory/SimpleStorage/Storage/EthernetInterfaces/NetworkInterfaces.*
	// are all absent from this fixture) — none of those keys should
	// survive into the final health dict.
	for _, absent := range []string{"Memory", "SimpleStorage", "Storage", "EthernetInterfaces", "NetworkPorts", "NetworkDeviceFunctions"} {
		if _, ok := health[absent]; ok {
			t.Fatalf("health should not include empty subsystem %q: %+v", absent, health)
		}
	}
}

func TestModuleRedfishInfoGetChassisHealthReport(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	chassisCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Chassis; rm -f /tmp/redfishtool-cfg.json`
	chassisJSON := `{"@odata.id":"/redfish/v1/Chassis/1/","Status":{"Health":"OK"},"Power":{"@odata.id":"/redfish/v1/Chassis/1/Power"},"Thermal":{"@odata.id":"/redfish/v1/Chassis/1/Thermal"},"Links":{"PCIeDevices":[{"@odata.id":"/redfish/v1/Chassis/1/PCIeDevices/1"}]}}`
	conn.on[chassisCmd] = remoteexec.Result{RC: 0, Stdout: chassisJSON}
	chassisRawCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Chassis/1/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[chassisRawCmd] = remoteexec.Result{RC: 0, Stdout: chassisJSON}
	powerCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Chassis/1/Power; rm -f /tmp/redfishtool-cfg.json`
	conn.on[powerCmd] = remoteexec.Result{RC: 0, Stdout: `{"PowerSupplies":[{"@odata.id":"/redfish/v1/Chassis/1/Power#/PowerSupplies/0","Status":{"Health":"OK"},"Name":"PS1"}]}`}
	thermalCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Chassis/1/Thermal; rm -f /tmp/redfishtool-cfg.json`
	conn.on[thermalCmd] = remoteexec.Result{RC: 0, Stdout: `{"Fans":[{"@odata.id":"/redfish/v1/Chassis/1/Thermal#/Fans/0","Status":{"Health":"OK"},"Name":"Fan1"}]}`}
	pcieCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Chassis/1/PCIeDevices/1; rm -f /tmp/redfishtool-cfg.json`
	conn.on[pcieCmd] = remoteexec.Result{RC: 0, Stdout: `{"Status":{"Health":"OK"}}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Chassis"}, "command": []any{"GetHealthReport"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	hrResult, _ := facts["health_report"].(map[string]any)
	if hrResult["ret"] != true {
		t.Fatalf("health_report = %+v", hrResult)
	}
	entries, _ := hrResult["entries"].([]any)
	pair, _ := entries[0].([]any)
	uriMap, _ := pair[0].(map[string]any)
	if uriMap["chassis_uri"] != "/redfish/v1/Chassis/1/" {
		t.Fatalf("uriMap = %+v", uriMap)
	}
	health, _ := pair[1].(map[string]any)
	// PowerSupplies/Fans both use real Ansible's own "expanded" shortcut
	// (their own @odata.id contains "#" and the embedded object carries
	// more than just that one key) — no separate GET of the PowerSupply/
	// Fan object itself should have been needed; only the two named
	// mocked commands (Power, Thermal) plus the PCIeDevices GET (a plain
	// link, no "#", needing a real GET) should have been issued.
	powerSupplies, _ := health["PowerSupplies"].([]any)
	if len(powerSupplies) != 1 {
		t.Fatalf("PowerSupplies = %+v, want 1", powerSupplies)
	}
	ps, _ := powerSupplies[0].(map[string]any)
	if ps["powersupply_uri"] != "/redfish/v1/Chassis/1/Power#/PowerSupplies/0" {
		t.Fatalf("ps = %+v", ps)
	}
	fans, _ := health["Fans"].([]any)
	if len(fans) != 1 {
		t.Fatalf("Fans = %+v, want 1", fans)
	}
	fan, _ := fans[0].(map[string]any)
	if fan["fan_uri"] != "/redfish/v1/Chassis/1/Thermal#/Fans/0" {
		t.Fatalf("fan = %+v", fan)
	}
	pcieDevices, _ := health["PCIeDevices"].([]any)
	if len(pcieDevices) != 1 {
		t.Fatalf("PCIeDevices = %+v, want 1", pcieDevices)
	}
	pcie, _ := pcieDevices[0].(map[string]any)
	if pcie["pciedevice_uri"] != "/redfish/v1/Chassis/1/PCIeDevices/1" {
		t.Fatalf("pcie = %+v", pcie)
	}
}

func TestModuleRedfishInfoGetManagerHealthReport(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	mgrCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Managers; rm -f /tmp/redfishtool-cfg.json`
	mgrJSON := `{"@odata.id":"/redfish/v1/Managers/1/","Status":{"Health":"OK"}}`
	conn.on[mgrCmd] = remoteexec.Result{RC: 0, Stdout: mgrJSON}
	mgrRawCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[mgrRawCmd] = remoteexec.Result{RC: 0, Stdout: mgrJSON}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Manager"}, "command": []any{"GetHealthReport"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	hrResult, _ := facts["health_report"].(map[string]any)
	if hrResult["ret"] != true {
		t.Fatalf("health_report = %+v", hrResult)
	}
	entries, _ := hrResult["entries"].([]any)
	pair, _ := entries[0].([]any)
	uriMap, _ := pair[0].(map[string]any)
	if uriMap["manager_uri"] != "/redfish/v1/Managers/1/" {
		t.Fatalf("uriMap = %+v", uriMap)
	}
	health, _ := pair[1].(map[string]any)
	manager, _ := health["Manager"].(map[string]any)
	status, _ := manager["Status"].(map[string]any)
	if status["Health"] != "OK" {
		t.Fatalf("Manager = %+v, want just the top-level Status (real get_manager_health_report has an empty subsystems list)", manager)
	}
	if len(health) != 1 {
		t.Fatalf("health = %+v, want only the Manager key", health)
	}
}

func TestModuleRedfishInfoGetNicInventorySystems(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	sysCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/"}`}
	sysRawCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysRawCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/","EthernetInterfaces":{"@odata.id":"/redfish/v1/Systems/1/EthernetInterfaces"}}`}
	listCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/EthernetInterfaces; rm -f /tmp/redfishtool-cfg.json`
	conn.on[listCmd] = remoteexec.Result{RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Systems/1/EthernetInterfaces/NIC1"}]}`}
	nicCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/EthernetInterfaces/NIC1; rm -f /tmp/redfishtool-cfg.json`
	conn.on[nicCmd] = remoteexec.Result{RC: 0, Stdout: `{"Name":"NIC.1","MACAddress":"de:ad:be:ef:00:01","Unrelated":"x"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"}, "command": []any{"GetNicInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	nicResult, _ := facts["nic"].(map[string]any)
	if nicResult["ret"] != true {
		t.Fatalf("nic = %+v", nicResult)
	}
	entries, _ := nicResult["entries"].([]any)
	pair, _ := entries[0].([]any)
	uriMap, _ := pair[0].(map[string]any)
	if uriMap["resource_uri"] != "/redfish/v1/Systems/1/" {
		t.Fatalf("uriMap = %+v", uriMap)
	}
	nics, _ := pair[1].([]any)
	if len(nics) != 1 {
		t.Fatalf("nics = %+v, want 1", nics)
	}
	nic, _ := nics[0].(map[string]any)
	if nic["MACAddress"] != "de:ad:be:ef:00:01" {
		t.Fatalf("nic = %+v", nic)
	}
	if _, ok := nic["Unrelated"]; ok {
		t.Fatalf("nic should not include unrelated properties: %+v", nic)
	}
}

func TestModuleRedfishInfoGetNicInventoryMissingKeySoftFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	sysCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/"}`}
	sysRawCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysRawCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"}, "command": []any{"GetNicInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want ok (a per-command problem is a soft embed, not a module failure)", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	nicResult, _ := facts["nic"].(map[string]any)
	if nicResult["ret"] != false {
		t.Fatalf("nic = %+v, want ret:false (EthernetInterfaces key missing)", nicResult)
	}
}

func TestModuleRedfishInfoGetVirtualMediaSystems(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	sysCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/"}`}
	sysRawCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysRawCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/","VirtualMedia":{"@odata.id":"/redfish/v1/Systems/1/VirtualMedia"}}`}
	listCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/VirtualMedia; rm -f /tmp/redfishtool-cfg.json`
	conn.on[listCmd] = remoteexec.Result{RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Systems/1/VirtualMedia/RemovableDisk1"}]}`}
	memberCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/VirtualMedia/RemovableDisk1; rm -f /tmp/redfishtool-cfg.json`
	conn.on[memberCmd] = remoteexec.Result{RC: 0, Stdout: `{"Id":"RemovableDisk1","Name":"Removable Disk","MediaTypes":["USBStick"]}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"}, "command": []any{"GetVirtualMedia"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	vmResult, _ := facts["virtual_media"].(map[string]any)
	if vmResult["ret"] != true {
		t.Fatalf("virtual_media = %+v", vmResult)
	}
	entries, _ := vmResult["entries"].([]any)
	pair, _ := entries[0].([]any)
	vm, _ := pair[1].([]any)
	if len(vm) != 1 {
		t.Fatalf("vm = %+v, want 1", vm)
	}
	entry, _ := vm[0].(map[string]any)
	if entry["Name"] != "Removable Disk" {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestModuleRedfishInfoGetCpuInventory(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	sysCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/"}`}
	sysRawCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysRawCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/","Processors":{"@odata.id":"/redfish/v1/Systems/1/Processors"}}`}
	listCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/Processors; rm -f /tmp/redfishtool-cfg.json`
	conn.on[listCmd] = remoteexec.Result{RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Systems/1/Processors/CPU1"}]}`}
	cpuCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/Processors/CPU1; rm -f /tmp/redfishtool-cfg.json`
	conn.on[cpuCmd] = remoteexec.Result{RC: 0, Stdout: `{"Model":"Intel Xeon","TotalCores":16,"Unrelated":"x"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"}, "command": []any{"GetCpuInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	cpuResult, _ := facts["cpu"].(map[string]any)
	if cpuResult["ret"] != true {
		t.Fatalf("cpu = %+v", cpuResult)
	}
	entries, _ := cpuResult["entries"].([]any)
	pair, _ := entries[0].([]any)
	cpus, _ := pair[1].([]any)
	if len(cpus) != 1 {
		t.Fatalf("cpus = %+v, want 1", cpus)
	}
	cpu, _ := cpus[0].(map[string]any)
	if cpu["Model"] != "Intel Xeon" || cpu["TotalCores"] != float64(16) {
		t.Fatalf("cpu = %+v", cpu)
	}
	if _, ok := cpu["Unrelated"]; ok {
		t.Fatalf("cpu should not include unrelated properties: %+v", cpu)
	}
}

func TestModuleRedfishInfoGetCpuInventoryMissingKeySoftFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	sysCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/"}`}
	sysRawCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysRawCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"}, "command": []any{"GetCpuInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want ok", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	cpuResult, _ := facts["cpu"].(map[string]any)
	if cpuResult["ret"] != false {
		t.Fatalf("cpu = %+v, want ret:false (Processors key missing)", cpuResult)
	}
}

func TestModuleRedfishInfoGetMemoryInventory(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	sysCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/"}`}
	sysRawCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysRawCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/","Memory":{"@odata.id":"/redfish/v1/Systems/1/Memory"}}`}
	listCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/Memory; rm -f /tmp/redfishtool-cfg.json`
	conn.on[listCmd] = remoteexec.Result{RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Systems/1/Memory/DIMM1"},{"@odata.id":"/redfish/v1/Systems/1/Memory/DIMM2"}]}`}
	dimm1Cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/Memory/DIMM1; rm -f /tmp/redfishtool-cfg.json`
	conn.on[dimm1Cmd] = remoteexec.Result{RC: 0, Stdout: `{"Status":{"State":"Enabled"},"CapacityMiB":16384,"Unrelated":"x"}`}
	dimm2Cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/Memory/DIMM2; rm -f /tmp/redfishtool-cfg.json`
	conn.on[dimm2Cmd] = remoteexec.Result{RC: 0, Stdout: `{"Status":{"State":"Absent"},"CapacityMiB":0}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"}, "command": []any{"GetMemoryInventory"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	memResult, _ := facts["memory"].(map[string]any)
	if memResult["ret"] != true {
		t.Fatalf("memory = %+v", memResult)
	}
	entries, _ := memResult["entries"].([]any)
	pair, _ := entries[0].([]any)
	dimms, _ := pair[1].([]any)
	// DIMM2 is Absent and should be filtered out entirely.
	if len(dimms) != 1 {
		t.Fatalf("dimms = %+v, want 1 (Absent DIMM filtered)", dimms)
	}
	dimm, _ := dimms[0].(map[string]any)
	if dimm["CapacityMiB"] != float64(16384) {
		t.Fatalf("dimm = %+v", dimm)
	}
	if _, ok := dimm["Unrelated"]; ok {
		t.Fatalf("dimm should not include unrelated properties: %+v", dimm)
	}
}

func TestModuleRedfishInfoGetBiosAttributes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	sysCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/"}`}
	sysRawCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysRawCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/","Bios":{"@odata.id":"/redfish/v1/Systems/1/Bios"}}`}
	biosCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/Bios; rm -f /tmp/redfishtool-cfg.json`
	conn.on[biosCmd] = remoteexec.Result{RC: 0, Stdout: `{"Attributes":{"BootMode":"Uefi","NumLock":"On"}}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"}, "command": []any{"GetBiosAttributes"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	biosResult, _ := facts["bios_attribute"].(map[string]any)
	if biosResult["ret"] != true {
		t.Fatalf("bios_attribute = %+v", biosResult)
	}
	entries, _ := biosResult["entries"].([]any)
	pair, _ := entries[0].([]any)
	attrs, _ := pair[1].(map[string]any)
	if attrs["BootMode"] != "Uefi" || attrs["NumLock"] != "On" {
		t.Fatalf("attrs = %+v", attrs)
	}
}

func TestModuleRedfishInfoGetBootOrder(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	sysCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/"}`}
	sysRawCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysRawCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/","Boot":{"BootOrder":["Boot0001","Boot0002"],"BootOptions":{"@odata.id":"/redfish/v1/Systems/1/BootOptions"}}}`}
	optListCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/BootOptions; rm -f /tmp/redfishtool-cfg.json`
	conn.on[optListCmd] = remoteexec.Result{RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Systems/1/BootOptions/Boot0001"}]}`}
	opt1Cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/BootOptions/Boot0001; rm -f /tmp/redfishtool-cfg.json`
	conn.on[opt1Cmd] = remoteexec.Result{RC: 0, Stdout: `{"BootOptionReference":"Boot0001","DisplayName":"Hard Disk"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"}, "command": []any{"GetBootOrder"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	bootOrderResult, _ := facts["boot_order"].(map[string]any)
	if bootOrderResult["ret"] != true {
		t.Fatalf("boot_order = %+v", bootOrderResult)
	}
	entries, _ := bootOrderResult["entries"].([]any)
	pair, _ := entries[0].([]any)
	devices, _ := pair[1].([]any)
	if len(devices) != 2 {
		t.Fatalf("devices = %+v, want 2", devices)
	}
	dev1, _ := devices[0].(map[string]any)
	if dev1["DisplayName"] != "Hard Disk" || dev1["BootOptionReference"] != "Boot0001" {
		t.Fatalf("dev1 = %+v", dev1)
	}
	// Boot0002 has no matching BootOptions member — real Ansible falls
	// back to a bare {"BootOptionReference": ref} entry.
	dev2, _ := devices[1].(map[string]any)
	if dev2["BootOptionReference"] != "Boot0002" {
		t.Fatalf("dev2 = %+v, want bare BootOptionReference fallback", dev2)
	}
	if len(dev2) != 1 {
		t.Fatalf("dev2 = %+v, want only BootOptionReference", dev2)
	}
}

func TestModuleRedfishInfoGetBootOrderMissingKeySoftFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	sysCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com -1 Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/"}`}
	sysRawCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysRawCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Systems/1/"}`}
	res, err := moduleRedfishInfo(context.Background(), conn, redfishArgs(map[string]any{
		"category": []any{"Systems"}, "command": []any{"GetBootOrder"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want ok", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	bootOrderResult, _ := facts["boot_order"].(map[string]any)
	if bootOrderResult["ret"] != false {
		t.Fatalf("boot_order = %+v, want ret:false (Boot/BootOrder key missing)", bootOrderResult)
	}
}
