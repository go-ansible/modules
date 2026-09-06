package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func redfishArgs(extra map[string]any) map[string]any {
	base := map[string]any{
		"baseuri":  "https://bmc.example.com",
		"username": "admin",
		"password": "secret",
	}
	for k, v := range extra {
		base[k] = v
	}
	return base
}

func lastCommand(conn *fakeConn) string {
	if len(conn.Commands) == 0 {
		return ""
	}
	return conn.Commands[len(conn.Commands)-1]
}

func TestModuleRedfishCommandPowerOn(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"PowerOn"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	got := lastCommand(conn)
	if !strings.Contains(got, "redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Systems reset On") {
		t.Fatalf("command = %q", got)
	}
	if !strings.Contains(got, `"user":"admin"`) || !strings.Contains(got, `"password":"secret"`) {
		t.Fatalf("command missing credentials: %q", got)
	}
}

func TestModuleRedfishCommandPowerCycleKeepsWholeName(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"PowerCycle"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), "Systems reset PowerCycle") {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishCommandPowerFullPowerCycleFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"PowerFullPowerCycle"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (no redfishtool resetType for FullPowerCycle)", res)
	}
}

func TestModuleRedfishCommandSetOneTimeBoot(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"SetOneTimeBoot"}, "bootdevice": "Cd",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), "Systems setBootOverride Once Cd") {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishCommandDisableBootOverrideNeedsNoDevice(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"DisableBootOverride"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), "Systems setBootOverride Disabled") {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishCommandUefiTargetFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"SetOneTimeBoot"}, "bootdevice": "UefiTarget", "uefi_target": "/0x31",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (UefiTarget needs a field redfishtool can't set)", res)
	}
}

func TestModuleRedfishCommandBootOverrideModeFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"SetOneTimeBoot"}, "bootdevice": "Cd", "boot_override_mode": "UEFI",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (boot_override_mode not settable)", res)
	}
}

func TestModuleRedfishCommandIndicatorLedSystems(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"IndicatorLedBlink"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), "Systems setIndicatorLed Blinking") {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishCommandIndicatorLedChassis(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Chassis", "command": []any{"IndicatorLedOff"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), "Chassis setIndicatorLed Off") {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishCommandInvalidCategoryFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Bogus", "command": []any{"PowerOn"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}

func TestModuleRedfishCommandNotYetWiredCategoryFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Accounts", "command": []any{"AddUser"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (Accounts not wired yet this batch)", res)
	}
}

func TestModuleRedfishCommandAuthTokenFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, map[string]any{
		"category": "Systems", "command": []any{"PowerOn"},
		"baseuri": "https://bmc.example.com", "auth_token": "sometoken",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (auth_token has no safe non-argv path)", res)
	}
}

func TestModuleRedfishCommandMissingBaseuriFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, map[string]any{
		"category": "Systems", "command": []any{"PowerOn"}, "username": "admin", "password": "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}

func TestModuleRedfishCommandRedfishtoolFailureSurfacesStderr(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	// Any command not scripted returns RC:0 from fakeConn by default, so
	// script the exact expected one to fail instead, to prove the module
	// surfaces a real redfishtool error rather than treating it as ok.
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com Systems reset On; rm -f /tmp/redfishtool-cfg.json`
	conn.on[cmd] = remoteexec.Result{RC: 1, Stderr: "Error, could not connect to rhost"}
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Systems", "command": []any{"PowerOn"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || !strings.Contains(res.Msg, "could not connect") {
		t.Fatalf("res = %+v", res)
	}
}
