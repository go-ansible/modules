package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRedfishConfigSetBootOrder(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"SetBootOrder"}, "boot_order": []any{"Boot0001", "Boot0002"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	last := lastCommand(conn)
	if !strings.Contains(last, "Systems patch") || !strings.Contains(last, `"BootOrder":["Boot0001","Boot0002"]`) {
		t.Fatalf("command = %q", last)
	}
	if !strings.Contains(last, `patch '{`) {
		t.Fatalf("expected patch body to be single-quoted: %q", last)
	}
}

func TestModuleRedfishConfigSetBootOrderMissingFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"SetBootOrder"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}

func TestModuleRedfishConfigSetPowerRestorePolicy(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"SetPowerRestorePolicy"}, "power_restore_policy": "AlwaysOn",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), `Systems patch '{"PowerRestorePolicy":"AlwaysOn"}'`) {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishConfigSetDefaultBootOrder(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	getCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[getCmd] = remoteexec.Result{RC: 0, Stdout: `{"Actions":{"#ComputerSystem.SetDefaultBootOrder":{"target":"/redfish/v1/Systems/1/Actions/ComputerSystem.SetDefaultBootOrder"}}}`}
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"SetDefaultBootOrder"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), "raw POST /redfish/v1/Systems/1/Actions/ComputerSystem.SetDefaultBootOrder") {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishConfigSetBiosDefaultSettings(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	sysCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysCmd] = remoteexec.Result{RC: 0, Stdout: `{"Bios":{"@odata.id":"/redfish/v1/Systems/1/Bios/"}}`}
	biosCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/Bios/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[biosCmd] = remoteexec.Result{RC: 0, Stdout: `{"Actions":{"#Bios.ResetBios":{"target":"/redfish/v1/Systems/1/Bios/Actions/Bios.ResetBios"}}}`}
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"SetBiosDefaultSettings"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), "raw POST /redfish/v1/Systems/1/Bios/Actions/Bios.ResetBios") {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishConfigEnableSecureBoot(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	sysCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysCmd] = remoteexec.Result{RC: 0, Stdout: `{"SecureBoot":{"@odata.id":"/redfish/v1/Systems/1/SecureBoot/"}}`}
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"EnableSecureBoot"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	last := lastCommand(conn)
	if !strings.Contains(last, "raw PATCH /redfish/v1/Systems/1/SecureBoot/") || !strings.Contains(last, `"SecureBootEnable":true`) {
		t.Fatalf("command = %q", last)
	}
}

func TestModuleRedfishConfigSetSecureBootFalse(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	sysCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysCmd] = remoteexec.Result{RC: 0, Stdout: `{"SecureBoot":{"@odata.id":"/redfish/v1/Systems/1/SecureBoot/"}}`}
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"SetSecureBoot"}, "secure_boot_enable": false,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), `"SecureBootEnable":false`) {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishConfigSetBiosAttributes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	sysCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysCmd] = remoteexec.Result{RC: 0, Stdout: `{"Bios":{"@odata.id":"/redfish/v1/Systems/1/Bios/"}}`}
	biosCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/Bios/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[biosCmd] = remoteexec.Result{RC: 0, Stdout: `{"Attributes":{"BootMode":"Uefi","NumLock":"On"},"@Redfish.Settings":{"SettingsObject":{"@odata.id":"/redfish/v1/Systems/1/Bios/Settings/"}}}`}
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"SetBiosAttributes"},
		"bios_attributes": map[string]any{"BootMode": "Bios", "NumLock": "On"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	last := lastCommand(conn)
	if !strings.Contains(last, "raw PATCH /redfish/v1/Systems/1/Bios/Settings/") {
		t.Fatalf("command = %q", last)
	}
	if !strings.Contains(last, `"BootMode":"Bios"`) {
		t.Fatalf("command missing changed attribute: %q", last)
	}
	if strings.Contains(last, "NumLock") {
		t.Fatalf("command should not include already-set NumLock attribute: %q", last)
	}
}

func TestModuleRedfishConfigSetBiosAttributesAlreadySetIsIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	sysCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Systems; rm -f /tmp/redfishtool-cfg.json`
	conn.on[sysCmd] = remoteexec.Result{RC: 0, Stdout: `{"Bios":{"@odata.id":"/redfish/v1/Systems/1/Bios/"}}`}
	biosCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Systems/1/Bios/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[biosCmd] = remoteexec.Result{RC: 0, Stdout: `{"Attributes":{"BootMode":"Uefi"},"@Redfish.Settings":{"SettingsObject":{"@odata.id":"/redfish/v1/Systems/1/Bios/Settings/"}}}`}
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"SetBiosAttributes"},
		"bios_attributes": map[string]any{"BootMode": "Uefi"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v, want ok and unchanged (idempotent)", res)
	}
}

func TestModuleRedfishConfigSetBiosAttributesMissingFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"SetBiosAttributes"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}

func TestModuleRedfishConfigInvalidCategoryFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Manager", "command": []any{"SetNetworkProtocols"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (Manager not wired yet)", res)
	}
}

func TestModuleRedfishConfigMissingBaseuriFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishConfig(context.Background(), conn, map[string]any{
		"category": "Systems", "command": []any{"SetBootOrder"}, "boot_order": []any{"Boot0001"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}
