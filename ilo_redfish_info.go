package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

var iloRedfishInfoCategories = map[string][]string{
	"Sessions": {"GetiLOSessions"},
}

// moduleIloRedfishInfo implements Ansible's `ilo_redfish_info` module.
// Real ilo_redfish_info.py implements exactly one command,
// GetiLOSessions (category=Sessions), via `iLORedfishUtils.
// get_ilo_sessions()`: GET SessionService/Sessions/, then GET each
// member, collecting {Description, Id, Name, UserName} — excluding
// whichever session URI is the CALLER's OWN just-opened Redfish session
// (`Oem.Hpe.Links.MySession`), so a task run never reports itself back
// as an active session.
//
// # ilorest mapping
//
// `ilorest rawget` (see ilo_common.go's own doc comment) reproduces
// this GET-then-GET-each-member walk exactly. This port's own MySession
// exclusion is DELIBERATELY DROPPED: this port never opens an
// `ilorest login` HTTP session of its own (every rawget here runs
// LOCAL/in-band — see redfish_common.go's own doc comment — which talks
// to the BMC over its local hardware channel, not a login'd HTTP Redfish
// session), so there is no "this task's own session" entry to exclude in
// the first place; every session GetiLOSessions finds is a genuine,
// independently-opened session, not this port's own. This is a
// consequence of the local/in-band architecture, not an oversight.
//
// Args: category (required list, e.g. ["Sessions"], or ["all"]);
// command (list, defaults to GetiLOSessions when omitted, matching real
// ilo_redfish_info.py's own CATEGORY_COMMANDS_DEFAULT, or "all");
// baseuri/username/password/auth_token/timeout accepted for shape
// compatibility, NO EFFECT (see redfish_common.go's own doc comment).
func moduleIloRedfishInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	categories := argStringList(args, "category")
	if len(categories) == 0 {
		return Result{}, errArg("ilo_redfish_info: missing required argument: category")
	}
	if len(categories) == 1 && categories[0] == "all" {
		categories = nil
		for c := range iloRedfishInfoCategories {
			categories = append(categories, c)
		}
	}
	if res, ok := iloRequireBinary(ctx, conn, "ilo_redfish_info"); !ok {
		return res, nil
	}

	result := map[string]any{}
	for _, category := range categories {
		if res, ok := redfishCheckCategory("ilo_redfish_info", category, iloRedfishInfoCategories); !ok {
			return res, nil
		}
		commands := argStringList(args, "command")
		if len(commands) == 0 {
			commands = []string{"GetiLOSessions"}
		} else if len(commands) == 1 && commands[0] == "all" {
			commands = iloRedfishInfoCategories[category]
		} else if res, ok := redfishCheckCommands("ilo_redfish_info", category, commands, iloRedfishInfoCategories); !ok {
			return res, nil
		}

		for _, command := range commands {
			if command != "GetiLOSessions" || category != "Sessions" {
				continue
			}
			out, err := iloGetSessions(ctx, conn)
			if err != nil {
				return Result{}, err
			}
			result[command] = out
		}
	}

	return Ok("").WithExtra("ilo_redfish_info", result), nil
}

func iloGetSessions(ctx context.Context, conn remoteexec.Connection) (map[string]any, error) {
	var coll struct {
		Members []struct {
			ODataID string `json:"@odata.id"`
		} `json:"Members"`
	}
	res, err := iloRawGet(ctx, conn, "/redfish/v1/SessionService/Sessions/", &coll)
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return map[string]any{"ret": false, "msg": iloErrMsg(res)}, nil
	}

	var sessions []map[string]any
	for _, m := range coll.Members {
		var data map[string]any
		sres, err := iloRawGet(ctx, conn, m.ODataID, &data)
		if err != nil {
			return nil, err
		}
		if sres.RC != 0 {
			continue
		}
		session := map[string]any{}
		for _, key := range []string{"Description", "Id", "Name", "UserName"} {
			if v, ok := data[key]; ok {
				session[key] = v
			}
		}
		sessions = append(sessions, session)
	}
	return map[string]any{"ret": true, "msg": sessions}, nil
}
