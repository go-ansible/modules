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
	bootOverride, _ := facts["boot_override"].([]any)
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
	bootOverride, _ := facts["boot_override"].([]any)
	pair, _ := bootOverride[0].([]any)
	entries, _ := pair[1].(map[string]any)
	if len(entries) != 0 {
		t.Fatalf("entries = %+v, want empty (override disabled)", entries)
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
	prp, _ := facts["power_restore_policy"].([]any)
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
