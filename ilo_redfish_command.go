package modules

import (
	"context"
	"fmt"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

var iloRedfishCommandCategories = map[string][]string{
	"Systems": {"WaitforiLORebootCompletion"},
}

// iloRebootPollAttempts/iloRebootPollIntervalSeconds bound this port's
// own WaitforiLORebootCompletion poll — see hwc_common.go's own
// hcloudPollJob doc comment for the identical precedent this follows:
// real WaitforiLORebootCompletion polls for up to 1800s (30 minutes,
// polling_interval=60s); this port does NOT block a single module
// invocation for that long. A reboot still in progress when this bound
// runs out is reported back as an unchanged, non-failed Ok (matching
// hcloudPollJob's own "already accepted, just couldn't confirm
// completion within this port's own shorter bound" framing), not a Fail.
const (
	iloRebootPollAttempts        = 5
	iloRebootPollIntervalSeconds = 2
)

// moduleIloRedfishCommand implements Ansible's `ilo_redfish_command`
// module. Real ilo_redfish_command.py implements exactly one command,
// WaitforiLORebootCompletion (category=Systems): poll the Systems
// resource's own Oem PostState (Hpe or, on older firmware, Hp) until it
// reaches "InPostDiscoveryComplete" or "FinishedPost", reporting
// Changed=true once it does, Changed=false if the server was already
// past POST when polling started, and Failed if the server is powered
// off or the poll times out.
//
// # ilorest mapping
//
// Reproduced via iloRawGet against the Systems collection's own first
// member (see ilo_common.go's own doc comment on ilorest's genuine raw
// GET/PATCH/POST support) — see this file's own doc comment above on
// this port's own shorter poll bound.
func moduleIloRedfishCommand(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	category, err := requireString(args, "category")
	if err != nil {
		return Result{}, err
	}
	commands := argStringList(args, "command")
	if len(commands) == 0 {
		return Result{}, errArg("ilo_redfish_command: missing required argument: command")
	}
	if res, ok := redfishCheckCategory("ilo_redfish_command", category, iloRedfishCommandCategories); !ok {
		return res, nil
	}
	if res, ok := redfishCheckCommands("ilo_redfish_command", category, commands, iloRedfishCommandCategories); !ok {
		return res, nil
	}
	if res, ok := iloRequireBinary(ctx, conn, "ilo_redfish_command"); !ok {
		return res, nil
	}

	result := map[string]any{}
	changed := false
	for _, command := range commands {
		if command != "WaitforiLORebootCompletion" {
			continue
		}
		out, err := iloWaitForRebootCompletion(ctx, conn)
		if err != nil {
			return Result{}, err
		}
		result[command] = out
		if c, _ := out["changed"].(bool); c {
			changed = true
		}
		if ok, _ := out["ret"].(bool); !ok {
			return Fail(fmt.Sprint(out)).WithExtra("ilo_redfish_command", result), nil
		}
	}
	return Result{Changed: changed}.WithExtra("ilo_redfish_command", result), nil
}

func iloSystemURI(ctx context.Context, conn remoteexec.Connection) (string, error) {
	var coll struct {
		Members []struct {
			ODataID string `json:"@odata.id"`
		} `json:"Members"`
	}
	res, err := iloRawGet(ctx, conn, "/redfish/v1/Systems/", &coll)
	if err != nil {
		return "", err
	}
	if res.RC != 0 || len(coll.Members) == 0 {
		return "", nil
	}
	return coll.Members[0].ODataID, nil
}

func iloServerPostState(ctx context.Context, conn remoteexec.Connection, systemURI string) (string, bool, error) {
	var data struct {
		Oem struct {
			Hpe struct {
				PostState string `json:"PostState"`
			} `json:"Hpe"`
			Hp struct {
				PostState string `json:"PostState"`
			} `json:"Hp"`
		} `json:"Oem"`
	}
	res, err := iloRawGet(ctx, conn, systemURI, &data)
	if err != nil {
		return "", false, err
	}
	if res.RC != 0 {
		return "", false, nil
	}
	if data.Oem.Hpe.PostState != "" {
		return data.Oem.Hpe.PostState, true, nil
	}
	return data.Oem.Hp.PostState, true, nil
}

func iloWaitForRebootCompletion(ctx context.Context, conn remoteexec.Connection) (map[string]any, error) {
	systemURI, err := iloSystemURI(ctx, conn)
	if err != nil {
		return nil, err
	}
	if systemURI == "" {
		return map[string]any{"ret": false, "msg": "could not find a Systems resource"}, nil
	}

	state, ok, err := iloServerPostState(ctx, conn, systemURI)
	if err != nil {
		return nil, err
	}
	if !ok {
		return map[string]any{"ret": false, "msg": "failed to read server PostState"}, nil
	}
	if state == "PowerOff" || state == "Off" {
		return map[string]any{"ret": false, "changed": false, "msg": "Server is powered OFF"}, nil
	}
	if state == "InPostDiscoveryComplete" || state == "FinishedPost" {
		return map[string]any{"ret": true, "changed": false, "msg": "Server is not rebooting"}, nil
	}

	for attempt := 0; attempt < iloRebootPollAttempts; attempt++ {
		if _, err := runStatus(ctx, conn, "sleep "+strconv.Itoa(iloRebootPollIntervalSeconds)); err != nil {
			return nil, err
		}
		state, ok, err = iloServerPostState(ctx, conn, systemURI)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if state == "InPostDiscoveryComplete" || state == "FinishedPost" {
			return map[string]any{"ret": true, "changed": true, "msg": "Server reboot is completed"}, nil
		}
	}
	return map[string]any{"ret": true, "changed": false, "msg": fmt.Sprintf("Server reboot not confirmed complete within this port's own shorter poll bound, server state: %s", state)}, nil
}
