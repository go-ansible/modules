package modules

import (
	"context"
	"regexp"

	remoteexec "github.com/go-remoteexec/transport"
)

// idracRedfishCommandCategories mirrors real idrac_redfish_command.py's
// own CATEGORY_COMMANDS_ALL exactly: only category=Systems,
// command=CreateBiosConfigJob is actually implemented upstream (Accounts
// and Manager are declared with an empty command list — i.e. real
// idrac_redfish_command currently accepts no commands for them either).
var idracRedfishCommandCategories = map[string][]string{
	"Systems":  {"CreateBiosConfigJob"},
	"Accounts": {},
	"Manager":  {},
}

// moduleIdracRedfishCommand implements Ansible's `idrac_redfish_command`
// module. Real idrac_redfish_command.py's own only implemented operation
// is CreateBiosConfigJob: schedule a job that applies whatever BIOS
// Setup attribute changes are currently PENDING on the iDRAC (set
// earlier, e.g. by idrac_redfish_config or a separate BIOS-attribute
// PATCH — this module itself never sets any BIOS attribute).
//
// # racadm mapping
//
// The exact, well-documented racadm equivalent of "commit pending BIOS
// Setup changes" is `racadm jobqueue create BIOS.Setup.1-1 -r <reboot>
// -s <start>` — BIOS.Setup.1-1 is the FQDD racadm/WSMAN both use for the
// BIOS pending-configuration object (a fixed name, not something
// resource_id changes: resource_id is accepted for argument-shape
// compatibility with real idrac_redfish_command's own System/Manager
// resource selection, but has no effect here, since there is exactly one
// BIOS.Setup FQDD per iDRAC generation this port could verify). This
// port passes `-r pwrcycle -s TIME_NOW`: apply on next power cycle,
// starting immediately — the same immediate-apply intent real
// CreateBiosConfigJob's own "schedule BIOS setting update" doc line
// describes, and the exact form independently confirmed via WebSearch
// against real-world racadm BIOS-commit examples before writing this
// file.
//
// Args: category (required, must be "Systems"); command (required list,
// each must be "CreateBiosConfigJob"); baseuri/username/password/
// auth_token/timeout/resource_id are accepted for argument-shape
// compatibility but have NO EFFECT (see redfish_common.go's own doc
// comment on this batch's local/in-band CLI architecture).
//
// Real return_values.job_id is a full Redfish Job resource URI (e.g.
// "/redfish/v1/Managers/iDRAC.Embedded.1/Jobs/JID_471269252011"); this
// port's own racadm-derived job_id is just the bare JID token racadm's
// own `jobqueue create` output prints (e.g. "JID_471269252011") — an
// honestly-documented shape difference, not a silent substitution: a
// caller wanting the bare ID either way is unaffected, one wanting the
// full URI shape is not.
func moduleIdracRedfishCommand(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	category, err := requireString(args, "category")
	if err != nil {
		return Result{}, err
	}
	commands := argStringList(args, "command")
	if len(commands) == 0 {
		return Result{}, errArg("idrac_redfish_command: missing required argument: command")
	}
	if res, ok := redfishCheckCategory("idrac_redfish_command", category, idracRedfishCommandCategories); !ok {
		return res, nil
	}
	if res, ok := redfishCheckCommands("idrac_redfish_command", category, commands, idracRedfishCommandCategories); !ok {
		return res, nil
	}
	if res, ok := racadmRequireBinary(ctx, conn, "idrac_redfish_command"); !ok {
		return res, nil
	}

	var jobID string
	for _, command := range commands {
		if command != "CreateBiosConfigJob" {
			continue
		}
		res, err := runStatus(ctx, conn, "racadm jobqueue create BIOS.Setup.1-1 -r pwrcycle -s TIME_NOW")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("idrac_redfish_command: CreateBiosConfigJob: " + racadmErrMsg(res)), nil
		}
		jobID = idracJobIDRe.FindString(res.Stdout)
	}

	out := Changed("Action was successful")
	if jobID != "" {
		out = out.WithExtra("return_values", map[string]any{"job_id": jobID})
	} else {
		out = out.WithExtra("return_values", map[string]any{})
	}
	return out, nil
}

var idracJobIDRe = regexp.MustCompile(`JID_[A-Za-z0-9]+`)
