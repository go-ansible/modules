package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what the three idrac_redfish_*.go modules share:
// shelling out to `racadm`, Dell's own official iDRAC/Lifecycle
// Controller CLI, instead of speaking Redfish HTTPS directly the way
// real idrac_redfish_command.py/idrac_redfish_config.py/
// idrac_redfish_info.py do via module_utils/_redfish_utils.py.
//
// # `racadm` is a real, official, but PARALLEL interface — not a
// # Redfish client
//
// `racadm` predates Dell's own Redfish support and does not speak the
// same category/command vocabulary real idrac_redfish_*'s own Redfish
// OEM extensions use — it addresses the iDRAC's attribute registry (the
// same underlying data store several Redfish OEM attributes ultimately
// read/write) via its own `<Group>[.<Index>].<Object>` addressing
// (Dell's own "Accessing Indexed-Based Device Groups and Objects"
// racadm reference, e.g. `racadm get nic.nicconfig.3.legacybootproto`),
// and manages jobs/actions via its own `jobqueue`/`serveraction`/`set`
// verbs. This port's job, per this batch's own instructions, is to
// identify the CLOSEST matching real racadm operation for each of the
// three real idrac_redfish_* modules' own (deliberately narrow —
// verified by reading each one's actual source, not assumed from a
// generic "Redfish OEM command" description) supported category/command
// set, and fail loud, per-operation, where no racadm equivalent exists.
// See each module's own file for its own specific mapping and any gaps.
//
// # Local, in-band racadm — see redfish_common.go
//
// This port always invokes LOCAL racadm (bare `racadm <subcommand>`,
// never `-r`/`-u`/`-p`) — see redfish_common.go's own doc comment for
// why (this exact same reasoning, and the same hponcfg.go precedent,
// applies to racadm here and to ilorest/OneCli in ilo_common.go/
// xcc_redfish_command.go).
//
// # The `iDRAC.` attribute-registry namespace is shared across Redfish
// # Manager/LifecycleController/System resources
//
// Real idrac_redfish_config's own SetManagerAttributes/
// SetLifecycleControllerAttributes/SetSystemAttributes each PATCH a
// DIFFERENT Redfish resource (Managers/iDRAC.Embedded.1/Attributes,
// Managers/LifecycleController.Embedded.1/Attributes, Managers/
// System.Embedded.1/Attributes respectively) — but all three resources
// expose views onto the SAME underlying iDRAC attribute registry, whose
// racadm addressing is uniformly the `idrac.` group namespace regardless
// of which Redfish resource also exposes a given attribute (Dell's own
// racadm reference documents LCAttributes.*/ServerPwr.*/SysLog.*/
// NTPConfigGroup.* etc. as `idrac.<Group>.<Index>.<Object>` regardless of
// attribute subject matter). Real idrac_redfish_config's own manager_
// attributes keys are ALREADY spelled in exactly this
// `<Group>.<Index>.<Object>` dotted shape (its own EXAMPLES:
// "NTPConfigGroup.1.NTPEnable", "LCAttributes.1.
// CollectSystemInventoryOnRestart", "ServerPwr.1.PSRedPolicy") — so this
// port issues `racadm set idrac.<key> <value>` uniformly for all three
// commands, one invocation per manager_attributes entry, which is a
// faithful mapping onto the SAME underlying attribute store real
// idrac_redfish_config's own three commands all ultimately reach, not a
// lossy approximation.
func racadmRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	return redfishRequireBinary(ctx, conn, moduleName, "racadm",
		"this port shells out to Dell's own local racadm CLI rather than speaking Redfish HTTPS directly — "+
			"see idrac_common.go's own doc comment and redfish_common.go's own doc comment on this batch's "+
			"local/in-band CLI architecture")
}

// racadmSet runs `racadm set idrac.<key> <value>` for one manager
// attribute — see idrac_common.go's own doc comment on why every
// idrac_redfish_config command maps onto this same `idrac.` namespace.
func racadmSet(ctx context.Context, conn remoteexec.Connection, key, value string) (remoteexec.Result, error) {
	cmd := "racadm set " + shellQuote("idrac."+key) + " " + shellQuote(value)
	return runStatus(ctx, conn, cmd)
}

func racadmErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}
