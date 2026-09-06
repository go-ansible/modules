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
		"category": "Manager", "command": []any{"GracefulRestart"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (Manager not wired yet this batch)", res)
	}
}

func TestModuleRedfishCommandUpdateUserNameNotYetWiredFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Accounts", "command": []any{"UpdateUserName"}, "account_username": "bob", "account_updatename": "robert",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (UpdateUserName needs id resolution not implemented)", res)
	}
}

func TestModuleRedfishCommandUpdateUserAccountTypesNotYetWiredFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Accounts", "command": []any{"UpdateUserAccountTypes"}, "account_username": "bob",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (no redfishtool primitive for account types)", res)
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

func TestModuleRedfishCommandClearSessionsEmpty(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Sessions", "command": []any{"ClearSessions"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v, want ok and unchanged", res)
	}
	if !strings.Contains(lastCommand(conn), "SessionService Sessions list") {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishCommandClearSessionsDeletesEach(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	listCmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com SessionService Sessions list; rm -f /tmp/redfishtool-cfg.json`
	conn.on[listCmd] = remoteexec.Result{RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/SessionService/Sessions/1/"},{"@odata.id":"/redfish/v1/SessionService/Sessions/2/"}]}`}
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Sessions", "command": []any{"ClearSessions"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	joined := strings.Join(conn.Commands, "\n")
	if !strings.Contains(joined, "-l /redfish/v1/SessionService/Sessions/1/ SessionService logout") ||
		!strings.Contains(joined, "-l /redfish/v1/SessionService/Sessions/2/ SessionService logout") {
		t.Fatalf("commands = %q", conn.Commands)
	}
}

func TestModuleRedfishCommandCreateSession(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com SessionService login; rm -f /tmp/redfishtool-cfg.json`
	conn.on[cmd] = remoteexec.Result{RC: 0, Stdout: `{"SessionId":"9","SessionLocation":"/redfish/v1/SessionService/Sessions/9/","X-Auth-Token":"abc123"}`}
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Sessions", "command": []any{"CreateSession"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	session, _ := res.Extra["session"].(map[string]any)
	if session["token"] != "abc123" || session["uri"] != "/redfish/v1/SessionService/Sessions/9/" {
		t.Fatalf("session = %+v", session)
	}
}

func TestModuleRedfishCommandCreateSessionMissingCredsFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, map[string]any{
		"baseuri": "https://bmc.example.com", "category": "Sessions", "command": []any{"CreateSession"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}

func TestModuleRedfishCommandDeleteSession(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Sessions", "command": []any{"DeleteSession"}, "session_uri": "/redfish/v1/SessionService/Sessions/9/",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), "-l /redfish/v1/SessionService/Sessions/9/ SessionService logout") {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishCommandDeleteSessionMissingURIFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Sessions", "command": []any{"DeleteSession"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}

func TestModuleRedfishCommandAddUser(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Accounts", "command": []any{"AddUser"},
		"account_username": "bob", "account_password": "hunter2", "account_roleid": "Operator",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), "AccountService adduser bob hunter2 Operator") {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishCommandAddUserAlreadyExistsIsIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com AccountService adduser bob hunter2; rm -f /tmp/redfishtool-cfg.json`
	conn.on[cmd] = remoteexec.Result{RC: 9, Stderr: "Error: username bob already exists"}
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Accounts", "command": []any{"AddUser"},
		"account_username": "bob", "account_password": "hunter2",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v, want ok and unchanged (idempotent)", res)
	}
}

func TestModuleRedfishCommandAddUserRejectsAccountTypes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Accounts", "command": []any{"AddUser"},
		"account_username": "bob", "account_password": "hunter2", "account_accounttypes": "Redfish",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (account_accounttypes not settable)", res)
	}
}

func TestModuleRedfishCommandDeleteUser(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Accounts", "command": []any{"DeleteUser"}, "account_username": "bob",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), "AccountService deleteuser bob") {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishCommandDeleteUserAlreadyGoneIsIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	cmd := `printf '%s' '{"password":"secret","user":"admin"}' > /tmp/redfishtool-cfg.json && redfishtool -c /tmp/redfishtool-cfg.json -r https://bmc.example.com AccountService deleteuser bob; rm -f /tmp/redfishtool-cfg.json`
	conn.on[cmd] = remoteexec.Result{RC: 9, Stderr: "Error: username bob does not exists"}
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Accounts", "command": []any{"DeleteUser"}, "account_username": "bob",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v, want ok and unchanged (idempotent)", res)
	}
}

func TestModuleRedfishCommandEnableUser(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Accounts", "command": []any{"EnableUser"}, "account_username": "bob",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), "AccountService useradmin bob enable") {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishCommandDisableUser(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Accounts", "command": []any{"DisableUser"}, "account_username": "bob",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), "AccountService useradmin bob disable") {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishCommandUpdateUserRole(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Accounts", "command": []any{"UpdateUserRole"}, "account_username": "bob", "account_roleid": "Administrator",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), "AccountService useradmin bob setRoleId Administrator") {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishCommandUpdateUserPassword(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Accounts", "command": []any{"UpdateUserPassword"}, "account_username": "bob", "account_password": "newpw",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), "AccountService setpassword bob newpw") {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishCommandUpdateAccountServiceProperties(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Accounts", "command": []any{"UpdateAccountServiceProperties"},
		"account_properties": map[string]any{"AuthFailureLoggingThreshold": 5},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if !strings.Contains(lastCommand(conn), `AccountService patch`) || !strings.Contains(lastCommand(conn), `AuthFailureLoggingThreshold`) {
		t.Fatalf("command = %q", lastCommand(conn))
	}
}

func TestModuleRedfishCommandUpdateAccountServicePropertiesMissingFailsLoud(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	res, err := moduleRedfishCommand(context.Background(), conn, redfishArgs(map[string]any{
		"category": "Accounts", "command": []any{"UpdateAccountServiceProperties"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}
