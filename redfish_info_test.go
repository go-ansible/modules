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
	manager, _ := facts["manager"].([]any)
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
	conn.on[mgrCmd] = remoteexec.Result{RC: 0, Stdout: `{"@odata.id":"/redfish/v1/Managers/1/","EthernetInterfaces":{"@odata.id":"/redfish/v1/Managers/1/EthernetInterfaces"}}`}
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
	nics, _ := facts["manager_nics"].([]any)
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
