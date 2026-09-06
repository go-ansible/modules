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
		"category": "Bogus", "command": []any{"SetNetworkProtocols"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (invalid category)", res)
	}
}

func TestModuleRedfishConfigSetManagerNicNotYetWiredFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Manager", "command": []any{"SetManagerNic"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (SetManagerNic not wired yet this batch)", res)
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

func TestModuleRedfishConfigSetServiceIdentification(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Manager", "command": []any{"SetServiceIdentification"}, "service_id": "rack1-u3",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), `Managers patch '{"ServiceIdentification":"rack1-u3"}'`) {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishConfigSetSessionService(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Sessions", "command": []any{"SetSessionService"},
		"sessions_config": map[string]any{"SessionTimeout": 600},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	last := lastCommand(conn)
	if !strings.Contains(last, "SessionService patch") || !strings.Contains(last, `"SessionTimeout":600`) {
		t.Fatalf("command = %q", last)
	}
}

func TestModuleRedfishConfigSetSessionServiceMissingFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Sessions", "command": []any{"SetSessionService"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}

func TestModuleRedfishConfigSetNetworkProtocols(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	getCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Managers; rm -f /tmp/redfishtool-cfg.json`
	conn.on[getCmd] = remoteexec.Result{RC: 0, Stdout: `{"NetworkProtocol":{"@odata.id":"/redfish/v1/Managers/1/NetworkProtocol/"}}`}
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Manager", "command": []any{"SetNetworkProtocols"},
		"network_protocols": map[string]any{"SSH": map[string]any{"ProtocolEnabled": "on", "Port": 22}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	last := lastCommand(conn)
	if !strings.Contains(last, "raw PATCH /redfish/v1/Managers/1/NetworkProtocol/") ||
		!strings.Contains(last, `"ProtocolEnabled":true`) || !strings.Contains(last, `"Port":22`) {
		t.Fatalf("command = %q", last)
	}
}

func TestModuleRedfishConfigSetNetworkProtocolsInvalidServiceFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Manager", "command": []any{"SetNetworkProtocols"},
		"network_protocols": map[string]any{"Bogus": map[string]any{"ProtocolEnabled": true}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (invalid service name)", res)
	}
}

func TestModuleRedfishConfigSetHostInterfaceSoleMember(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	mgrCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Managers; rm -f /tmp/redfishtool-cfg.json`
	conn.on[mgrCmd] = remoteexec.Result{RC: 0, Stdout: `{"HostInterfaces":{"@odata.id":"/redfish/v1/Managers/1/HostInterfaces/"}}`}
	listCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1/HostInterfaces/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[listCmd] = remoteexec.Result{RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Managers/1/HostInterfaces/1/"}]}`}
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Manager", "command": []any{"SetHostInterface"},
		"hostinterface_config": map[string]any{"InterfaceEnabled": true},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), "raw PATCH /redfish/v1/Managers/1/HostInterfaces/1/") {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishConfigSetHostInterfaceByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	mgrCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Managers; rm -f /tmp/redfishtool-cfg.json`
	conn.on[mgrCmd] = remoteexec.Result{RC: 0, Stdout: `{"HostInterfaces":{"@odata.id":"/redfish/v1/Managers/1/HostInterfaces/"}}`}
	listCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1/HostInterfaces/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[listCmd] = remoteexec.Result{RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Managers/1/HostInterfaces/1/"},{"@odata.id":"/redfish/v1/Managers/1/HostInterfaces/2/"}]}`}
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Manager", "command": []any{"SetHostInterface"}, "hostinterface_id": "2",
		"hostinterface_config": map[string]any{"InterfaceEnabled": true},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), "raw PATCH /redfish/v1/Managers/1/HostInterfaces/2/") {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishConfigSetHostInterfaceAmbiguousFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	mgrCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Managers; rm -f /tmp/redfishtool-cfg.json`
	conn.on[mgrCmd] = remoteexec.Result{RC: 0, Stdout: `{"HostInterfaces":{"@odata.id":"/redfish/v1/Managers/1/HostInterfaces/"}}`}
	listCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com raw GET /redfish/v1/Managers/1/HostInterfaces/; rm -f /tmp/redfishtool-cfg.json`
	conn.on[listCmd] = remoteexec.Result{RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Managers/1/HostInterfaces/1/"},{"@odata.id":"/redfish/v1/Managers/1/HostInterfaces/2/"}]}`}
	res, err := moduleRedfishConfig(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Manager", "command": []any{"SetHostInterface"},
		"hostinterface_config": map[string]any{"InterfaceEnabled": true},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (ambiguous, multiple interfaces)", res)
	}
}

func TestRedfishProtocolBoolCoercions(t *testing.T) {
	cases := []struct {
		in     any
		want   bool
		wantOK bool
	}{
		{true, true, true},
		{false, false, true},
		{"true", true, true},
		{"True", true, true},
		{"on", true, true},
		{"false", false, true},
		{"False", false, true},
		{"off", false, true},
		{1, true, true},
		{0, false, true},
		{float64(1), true, true},
		{float64(0), false, true},
		{"bogus", false, false},
		{2, false, false},
	}
	for _, c := range cases {
		got, ok := redfishProtocolBool(c.in)
		if ok != c.wantOK || (ok && got != c.want) {
			t.Errorf("redfishProtocolBool(%v) = (%v, %v), want (%v, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}
