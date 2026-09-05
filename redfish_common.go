package modules

import (
	"context"
	"fmt"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what the eight *_redfish_*.go modules in this
// batch (idrac_redfish_command, idrac_redfish_config, idrac_redfish_info,
// ilo_redfish_command, ilo_redfish_config, ilo_redfish_info,
// xcc_redfish_command, and hponcfg's own BMC-vendor siblings) share:
// real community.general Redfish OEM modules all talk to the BMC's own
// Redfish service directly over HTTPS (module_utils/_redfish_utils.py's
// RedfishUtils, a hand-rolled HTTP client keyed on baseuri/username/
// password/auth_token) — this port instead shells out to each vendor's
// own OFFICIAL local management CLI (Dell's `racadm`, HPE's `ilorest`,
// Lenovo's OneCLI's `OneCli`), one substitution per vendor, documented in
// full in each module's own file.
//
// # Architecture: LOCAL, in-band CLI, not a networked Redfish client
//
// All three vendor CLIs this batch uses genuinely support two distinct
// modes: a REMOTE mode addressing a BMC over the network by IP with
// explicit credentials (racadm's `-r <ip> -u <user> -p <password>`,
// ilorest's `--url=<ip> -u <user> -p <password>`, OneCLI's `--bmc
// <user>:<password>@<ip>`) — the same network-addressed shape real
// baseuri/username/password already imply — and a LOCAL/in-band mode,
// run directly on the managed server's OWN operating system, which talks
// to that same server's own on-board BMC over its local hardware channel
// (KCS/USB-NIC) and needs NO username/password at all, authenticating
// implicitly via the local OS's own root/administrator privilege
// instead. This port uses LOCAL/in-band mode exclusively, for one
// concrete, load-bearing reason: this project's hard "no secrets in
// argv" rule has no safe way to satisfy the REMOTE mode's own
// credential-on-the-command-line requirement for these three tools
// specifically — unlike xcli (XIV_XCLIPASSWORD) or redis-cli
// (REDISCLI_AUTH), this port found no officially-documented
// environment-variable or stdin alternative for racadm's `-p`,
// ilorest's `-p`, or OneCLI's `--bmc-password` during this batch's own
// research (WebSearch/WebFetch against each vendor's own CLI reference),
// and inventing one would be exactly the kind of unverified guess this
// project's bibliography-before-implementing rule forbids. LOCAL mode
// sidesteps the problem entirely, and is no less real: it is the SAME
// architecture this exact batch's own `hponcfg` module already uses
// (hponcfg.go — the real, upstream hponcfg.py module itself only ever
// runs locally on the managed server), and Dell/HPE/Lenovo each document
// their own local mode as a first-class, supported way to run their CLI.
//
// The practical consequence, documented again in each module's own file:
// baseuri/username/password/auth_token are all accepted, for
// argument-shape compatibility with real playbooks (which always target
// a REMOTE BMC over the network), but have NO EFFECT on this port's
// command construction — this port's Connection target IS the managed
// server whose own local racadm/ilorest/OneCli is invoked, needing no
// network address or credential of its own. This is a deliberate,
// consistently-applied, and honestly-documented architectural
// substitution, not a per-module oversight.
//
// # category/command validation
//
// Every real *_redfish_*.py module in this family validates category
// against a `CATEGORY_COMMANDS_ALL` map and every requested command
// against that category's own allowed list, failing with an identical
// message shape either way — this port reproduces both the validation
// and the exact message wording via redfishCheckCategory/
// redfishCheckCommands below.
func redfishCheckCategory(moduleName, category string, allowed map[string][]string) (Result, bool) {
	if _, ok := allowed[category]; ok {
		return Result{}, true
	}
	cats := make([]string, 0, len(allowed))
	for c := range allowed {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	return Fail(fmt.Sprintf("%s: Invalid Category '%s'. Valid Categories = %s", moduleName, category, formatPyList(cats))), false
}

func redfishCheckCommands(moduleName, category string, commands []string, allowed map[string][]string) (Result, bool) {
	valid := allowed[category]
	for _, cmd := range commands {
		if !stringSliceContains(valid, cmd) {
			return Fail(fmt.Sprintf("%s: Invalid Command '%s'. Valid Commands = %s", moduleName, cmd, formatPyList(valid))), false
		}
	}
	return Result{}, true
}

// formatPyList renders a []string the way Python's own str(list) would
// (e.g. "['A', 'B']"), matching the exact message shape real
// *_redfish_*.py modules produce via an f-string over a Python list —
// cosmetic, but worth matching exactly since these are the literal
// strings a playbook author's own error output would show.
func formatPyList(items []string) string {
	quoted := make([]string, len(items))
	for i, it := range items {
		quoted[i] = "'" + it + "'"
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// redfishRequireBinary fails cleanly (Result{Failed:true}, not a Go
// error) if binary is not on the target's PATH.
func redfishRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName, binary, explain string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v "+binary); err != nil {
		return Fail(fmt.Sprintf("%s: the %s binary is required on the target and was not found in PATH — %s",
			moduleName, binary, explain)), false
	}
	return Result{}, true
}
