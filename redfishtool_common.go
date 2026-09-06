package modules

import (
	"context"
	"encoding/json"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what redfish_command.go, redfish_config.go, and
// redfish_info.go share: shelling out to DMTF's own `redfishtool`
// (github.com/DMTF/Redfishtool) — the Redfish standard's own authoring
// body's reference CLI client, not a third-party or vendor tool.
//
// Unlike this batch's vendor-specific substitutions (racadm/ilorest/
// OneCli — see redfish_common.go's own doc comment), real
// redfish_command/config/info are vendor-NEUTRAL: they talk to whatever
// BMC's baseuri a playbook gives them, over the network, with real
// credentials. redfishtool matches that shape exactly — this port runs
// it in genuine REMOTE mode, and baseuri/username/password have REAL
// EFFECT here, unlike the local/in-band vendor CLIs' "accepted but
// ignored" shape.
//
// # Credentials: -c cfgFile, never argv
//
// redfishtool's own `-u`/`-p` flags put a password on argv, which this
// project's no-secrets-in-argv rule forbids. redfishtool also supports
// `-c <cfgFile>`, reading `{"user":..., "password":...}` from a JSON
// file — confirmed directly from redfishtoolMain.py's own source
// (`json.load(f)`, checking for `user`/`password` keys) before writing
// this file, not guessed. This port stages that JSON in a temp file on
// the target (via conn.TempPath, the same "everything travels through
// one Exec command string" pattern ilo_common.go's own iloRawWrite
// already established for a different tool) and points redfishtool at
// the file's path, deleting it immediately after.
//
// auth_token (bearer-session auth) has no safe non-argv path confirmed
// for redfishtool — only `-t <token>`, on argv — so a task using
// auth_token instead of username/password fails loud rather than
// putting a token in argv.
func redfishtoolRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	return redfishRequireBinary(ctx, conn, moduleName, "redfishtool",
		"this port shells out to DMTF's own local redfishtool CLI (github.com/DMTF/Redfishtool) rather than "+
			"speaking Redfish HTTPS directly — see redfishtool_common.go's own doc comment")
}

// redfishtoolRun runs `redfishtool -c <cfgFile> -r <baseuri> <args...>`
// against a real, networked Redfish service, staging username/password
// in a temp JSON file rather than argv.
func redfishtoolRun(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string, args ...string) (remoteexec.Result, error) {
	cfg := map[string]string{"user": username, "password": password}
	b, err := json.Marshal(cfg)
	if err != nil {
		return remoteexec.Result{}, err
	}
	tmp := conn.TempPath("redfishtool-cfg.json")
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shellQuote(a)
	}
	cmd := "printf '%s' " + shellQuote(string(b)) + " > " + shellQuote(tmp) +
		" && redfishtool -c " + shellQuote(tmp) + " -r " + shellQuote(baseuri) + " " + strings.Join(quoted, " ") +
		"; rm -f " + shellQuote(tmp)
	return runStatus(ctx, conn, cmd)
}

func redfishtoolErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}

// redfishtoolCredentials extracts baseuri/username/password from args
// and fails loud if auth_token is used instead (see this file's own doc
// comment on why that path isn't supported).
func redfishtoolCredentials(moduleName string, args map[string]any) (baseuri, username, password string, res Result, ok bool) {
	baseuri = argString(args, "baseuri", "")
	if baseuri == "" {
		return "", "", "", Fail(moduleName + ": missing required argument: baseuri"), false
	}
	username = argString(args, "username", "")
	password = argString(args, "password", "")
	if username == "" && password == "" && argString(args, "auth_token", "") != "" {
		return "", "", "", Fail(moduleName + ": auth_token is not supported by this port's redfishtool substitution " +
			"— no safe non-argv way to pass a session token was confirmed (see redfishtool_common.go's own doc " +
			"comment); use username/password instead"), false
	}
	return baseuri, username, password, Result{}, true
}
