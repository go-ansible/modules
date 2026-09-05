package modules

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleEmcVnxSgMember implements Ansible's `emc_vnx_sg_member` module:
// adds or removes a single LUN (by its system LUN number, "ALU") to/from
// an existing EMC/Dell VNX storage group.
//
// EMC VNX is legacy, EOL-adjacent Dell storage hardware (end-of-support
// per Dell's own VNX series lifecycle notices) — the same honest framing
// this port already gives AIX (aix_devices.go/aix_filesystem.go) for
// hardware whose own vendor documentation is itself aging. It is
// included here because it is still a REAL community.general module
// with a genuine, still-supported vendor CLI behind it.
//
// # Substitution: `naviseccli` instead of storops
//
// Real emc_vnx_sg_member.py talks to the VNX Storage Processor's own
// management API through `storops` (a Python client library over VNX's
// XML-RPC-ish "Navisphere" API, authenticating per-invocation with
// sp_address/sp_user/sp_password). This port instead shells out to
// `naviseccli` ("Navisphere Secure CLI"), Dell/EMC's own official,
// still-documented VNX block CLI (EMC VNX Series Command Line Interface
// Reference for Block) — the same tool OpenStack Cinder's own VNX
// driver shells out to for exactly this operation, confirmed against
// that CLI reference's own `storagegroup` command page before writing
// this file (see the exact SYNTAX excerpts below), not guessed from the
// module name.
//
// Exact naviseccli syntax used, verified against the CLI reference:
//
//	naviseccli -h <sp_address> storagegroup -list -gname <name>
//	naviseccli -h <sp_address> storagegroup -addhlu -gname <name> -hlu <hlu> -alu <alu>
//	naviseccli -h <sp_address> storagegroup -removehlu -gname <name> -hlu <hlu>
//
// `-list -gname <name>`'s own output includes a "HLU/ALU Pairs:" table
// (a "HLU Number"/"ALU Number" two-column list) for every LUN currently
// in the group — this port parses that table (hluAluPairs below) to
// decide whether the requested ALU (lunid) is already a member, and to
// discover an existing HLU/ALU pair's own HLU for -removehlu/absent.
//
// # Auth precondition, and the HLU-allocation deviation this forces
//
// The CLI reference's own naviseccli global OPTIONS document
// `-AddUserSecurity` (with `-user`/`-password`/`-scope`) as the way to
// cache a login in a per-OS-user "security file" so that EVERY
// subsequent naviseccli invocation on that host needs no `-user`/
// `-password` at all — naviseccli's own documented alternative to
// putting a password on the command line ("You can omit this switch if
// you are using a security file. See -AddUserSecurity."), the same
// shape of precedent ipa_common.go's own doc comment documents for
// `ipa`/`kinit`. Consistent with this project's hard "no secrets in
// argv" rule, this port therefore:
//   - NEVER places sp_password on a naviseccli command line, or in an
//     environment variable it sets itself.
//   - requires that `naviseccli -h <sp_address> -AddUserSecurity -user
//     <user> -scope 0` (interactively prompting for the password, or
//     with -password supplied out of band by whoever runs it) has
//     ALREADY been run, once, for the OS user this module's Connection
//     executes as, before this module runs against that sp_address —
//     matching this project's established "the target's own tool must
//     already be authenticated" convention (ipa_common.go/
//     gitlab_common.go/keycloak_common.go all document the same shape).
//   - accepts sp_user/sp_password (default "sysadmin"/"sysadmin",
//     matching real emc_vnx_sg_member's own defaults) for argument-shape
//     compatibility with real playbooks, but they have NO EFFECT: this
//     port's naviseccli invocations never reference them.
//
// Real emc_vnx_sg_member's own storops-based attach_alu() picks the new
// LUN's host-visible HLU number automatically (storops/the VNX API
// itself assigns it); naviseccli's own -addhlu has no such "pick one for
// me" mode — -hlu is a required, explicit argument (see SYNTAX above).
// This is a genuine architecture gap, not an oversight: this port picks
// the LOWEST HLU NUMBER NOT ALREADY IN USE in the target storage group's
// own current HLU/ALU table (starting at 0) and passes that explicitly,
// which is the same "next free slot" allocation policy the VNX API's own
// automatic assignment is documented to use. The chosen number is
// returned as `hluid`, matching real emc_vnx_sg_member's own `hluid`
// return value's documented meaning ("LUNID visible to hosts attached to
// the storage group").
func moduleEmcVnxSgMember(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	spAddress, err := requireString(args, "sp_address")
	if err != nil {
		return Result{}, err
	}
	if _, ok := args["lunid"]; !ok {
		return Result{}, errArg("emc_vnx_sg_member: missing required argument: lunid")
	}
	aluN := argInt(args, "lunid", -1)
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent":
	default:
		return Result{}, errArg("emc_vnx_sg_member: state must be present or absent, got %q", state)
	}
	// sp_user/sp_password: accepted for shape compatibility, no effect —
	// see this file's own doc comment.

	listCmd := "naviseccli -h " + shellQuote(spAddress) + " storagegroup -list -gname " + shellQuote(name)
	res, err := runStatus(ctx, conn, listCmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("No such storage group named %s", name)), nil
	}
	pairs := hluAluPairs(res.Stdout)

	switch state {
	case "present":
		for hlu, thisAlu := range pairs {
			if thisAlu == aluN {
				return Ok("").WithExtra("hluid", hlu), nil
			}
		}
		hlu := firstFreeHlu(pairs)
		addCmd := "naviseccli -h " + shellQuote(spAddress) + " storagegroup -addhlu -gname " + shellQuote(name) +
			" -hlu " + strconv.Itoa(hlu) + " -alu " + strconv.Itoa(aluN)
		ares, err := runStatus(ctx, conn, addCmd)
		if err != nil {
			return Result{}, err
		}
		if ares.RC != 0 {
			return Fail(fmt.Sprintf("Error attaching %d: %s", aluN, strings.TrimSpace(firstNonEmpty(ares.Stderr, ares.Stdout)))), nil
		}
		return Changed("").WithExtra("hluid", hlu), nil

	default: // absent
		for hlu, thisAlu := range pairs {
			if thisAlu == aluN {
				rmCmd := "naviseccli -h " + shellQuote(spAddress) + " storagegroup -removehlu -gname " + shellQuote(name) +
					" -hlu " + strconv.Itoa(hlu)
				rres, err := runStatus(ctx, conn, rmCmd)
				if err != nil {
					return Result{}, err
				}
				if rres.RC != 0 {
					return Fail(fmt.Sprintf("Error detaching alu %d: %s", aluN, strings.TrimSpace(firstNonEmpty(rres.Stderr, rres.Stdout)))), nil
				}
				return Changed(""), nil
			}
		}
		return Ok(""), nil
	}
}

var hluAlulLineRe = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s*$`)

// hluAluPairs parses the "HLU/ALU Pairs:" table out of `storagegroup
// -list -gname <name>`'s own output (see this file's own doc comment for
// an example of the table's exact shape) into hlu -> alu.
func hluAluPairs(output string) map[int]int {
	out := map[int]int{}
	lines := strings.Split(output, "\n")
	inTable := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "HLU/ALU Pairs"):
			inTable = true
			continue
		case strings.HasPrefix(trimmed, "HLU Number"):
			continue
		case strings.HasPrefix(trimmed, "----"):
			continue
		}
		if !inTable {
			continue
		}
		if trimmed == "" {
			// A blank line ends the table UNLESS we haven't seen any
			// data rows yet (the "HLU Number ... / ----" header lines
			// above are also separated by a blank line in real output).
			if len(out) > 0 {
				break
			}
			continue
		}
		m := hluAlulLineRe.FindStringSubmatch(line)
		if m == nil {
			// A non-matching, non-blank line (e.g. "Shareable: YES")
			// ends the table.
			if len(out) > 0 {
				break
			}
			continue
		}
		hlu, _ := strconv.Atoi(m[1])
		alu, _ := strconv.Atoi(m[2])
		out[hlu] = alu
	}
	return out
}

// firstFreeHlu returns the lowest non-negative HLU number not already a
// key of used — see this file's own doc comment on why this port must
// pick one explicitly.
func firstFreeHlu(used map[int]int) int {
	for hlu := 0; ; hlu++ {
		if _, taken := used[hlu]; !taken {
			return hlu
		}
	}
}
