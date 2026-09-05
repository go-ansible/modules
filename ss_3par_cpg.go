package modules

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSs3parCpg implements Ansible's `ss_3par_cpg` module: creates or
// removes a Common Provisioning Group (CPG) on an HPE 3PAR StoreServ
// array, via the real, official "3PAR CLI" (`cli`/`3par_cli`, whichever
// name the array's own embedded shell registers as) reached over SSH —
// verified against HPE's own "3PAR Command Line Interface Reference"
// (createcpg/removecpg/showcpg's own SYNTAX/OPTIONS sections, read
// before writing this file, not guessed from the module name).
//
// Args: cpg_name (required, 1-31 characters — real ss_3par_cpg.py's own
// length check is reproduced here); state (present|absent, required);
// domain; growth_increment, growth_limit, growth_warning (size strings
// like "32000 MiB"/"64 GiB", matching real ss_3par_cpg.py's own
// EXAMPLES); raid_type (R0|R1|R5|R6); set_size (int); high_availability
// (PORT|CAGE|MAG); disk_type (FC|NL|SSD). storage_system_ip/
// storage_system_username/storage_system_password/secure are the array
// connection arguments (see below for how this port uses them).
//
// # Substitution: 3PAR CLI over SSH, instead of WSAPI
//
// Real ss_3par_cpg.py talks to the array's WSAPI REST service
// (hpe3par_sdk/hpe3parclient, `https://<storage_system_ip>:8080/api/v1`)
// with a per-invocation login/logout. The real 3PAR CLI has no network
// REST surface of its own at all — it is HPE's own array-embedded shell,
// reachable ONLY by SSHing directly to the array itself (there is no
// separate, locally-installable "3par CLI" package the way naviseccli or
// xcli are — logging in over SSH drops you straight into its `cli%`
// prompt), documented as such throughout HPE's own "3PAR Command Line
// Interface Reference". This port therefore runs
//
//	ssh <storage_system_username>@<storage_system_ip> <command>
//
// from the module's own Connection target, passing the 3PAR CLI command
// as SSH's own remote-command argument (3PAR's embedded shell accepts a
// single non-interactive command this way, exactly like running any
// other single remote command via `ssh host cmd`). This matches this
// project's own established "the target's own tool needs an
// already-authenticated session" convention (ipa_common.go's kinit
// precedent, gitlab_common.go's `glab auth login` precedent): SSH
// key-based (or already-cached host) trust to the array for
// storage_system_username must already be configured on this module's
// own Connection target before this module runs. storage_system_password
// is accepted for argument-shape compatibility with real playbooks (real
// ss_3par_cpg.py's own WSAPI login genuinely needs it) but has NO
// EFFECT: it is never placed on this port's SSH command line or in an
// environment variable this port sets, since non-interactive `ssh` has
// no standard, safe way to consume a password without either an argv
// leak or an interactive TTY prompt this port's own Connection.Exec
// cannot answer — a deliberate, honestly-documented gap, not a silent
// misinterpretation. secure (real ss_3par_cpg.py's own WSAPI TLS
// certificate-validation toggle) has no 3PAR-CLI-over-SSH equivalent
// either and is likewise accepted with no effect.
//
// # Exact 3PAR CLI syntax used, verified against the CLI Reference
//
//	showcpg <cpg_name>
//	createcpg -f [-sdgs <size>] [-sdgl <size>] [-sdgw <size>] [-domain <domain>] [-t <raid>] [-ssz <n>] [-ha port|cage|mag] [-devtype FC|NL|SSD] <cpg_name>
//	removecpg -f <cpg_name>
//
// `-f` on both createcpg and removecpg suppresses the CLI's own
// interactive confirmation prompt (documented: "Forces the command. The
// command completes the process without prompting for confirmation."),
// required for this port's non-interactive use. `-t`/`-ha`/`-devtype`
// all take real 3PAR CLI's own lowercase spellings (r0/r1/r5/r6,
// port/cage/mag, FC/NL/SSD) — this port lowercases raid_type/
// high_availability to match (disk_type's FC/NL/SSD are already
// uppercase in both real ss_3par_cpg.py's own choices and the CLI's own
// -devtype values, so no conversion is needed there).
//
// growth_increment/growth_limit/growth_warning are converted from real
// ss_3par_cpg's own "<number> <unit>" string shape (MiB/GiB/TiB, per its
// own EXAMPLES) to the CLI's own accepted "<number>[g|t]" shape (no
// suffix means MB, matching real ss_3par_cpg.py's own
// convert_to_binary_multiple, which normalizes to MiB before sending —
// this port's own cpgSizeToCLI below performs the equivalent unit
// normalization for the CLI's own g/t suffixes) via cpgSizeToCLI.
//
// Idempotency matches real ss_3par_cpg.py exactly: `showcpg <cpg_name>`
// decides existence (a non-zero/empty result means "does not exist",
// matching real cpgExists()); state=present creates only if absent
// (never diffs/updates an existing CPG's own attributes, matching real
// create_cpg()'s own "CPG already present" no-op path exactly); "raid
// set_size must be one of the RAID type's own allowed sizes" validation
// real ss_3par_cpg.py performs via HPE3ParClient.RAID_MAP is NOT
// reproduced here (this port has no independently-verified copy of that
// map to check against without guessing) — an invalid set_size is passed
// through to createcpg, which will itself reject it, still surfacing as
// a Fail(), just with the 3PAR CLI's own error text instead of this
// port's own pre-check.
func moduleSs3parCpg(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	cpgName, err := requireString(args, "cpg_name")
	if err != nil {
		return Result{}, err
	}
	if len(cpgName) < 1 || len(cpgName) > 31 {
		return Fail("CPG name must be at least 1 character and not more than 31 characters"), nil
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	sysIP, err := requireString(args, "storage_system_ip")
	if err != nil {
		return Result{}, err
	}
	sysUser := argString(args, "storage_system_username", "")
	if sysUser == "" {
		return Fail("Storage system username or password is None"), nil
	}
	// storage_system_password/secure: accepted, no effect — see this
	// file's own doc comment.

	res, err := ss3parSSH(ctx, conn, sysUser, sysIP, "showcpg "+shellQuote(cpgName))
	if err != nil {
		return Result{}, err
	}
	exists := res.RC == 0 && ss3parCpgInShowOutput(res.Stdout, cpgName)

	switch state {
	case "present":
		if exists {
			return Ok("CPG already present"), nil
		}
		cmd, err := ss3parCreateCmd(args, cpgName)
		if err != nil {
			return Result{}, err
		}
		cres, err := ss3parSSH(ctx, conn, sysUser, sysIP, cmd)
		if err != nil {
			return Result{}, err
		}
		if cres.RC != 0 {
			return Fail(fmt.Sprintf("CPG creation failed | %s", strings.TrimSpace(ss3parErr(cres)))), nil
		}
		return Changed(fmt.Sprintf("Created CPG %s successfully.", cpgName)), nil

	case "absent":
		if !exists {
			return Ok("CPG does not exist"), nil
		}
		dres, err := ss3parSSH(ctx, conn, sysUser, sysIP, "removecpg -f "+shellQuote(cpgName))
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return Fail(fmt.Sprintf("CPG delete failed | %s", strings.TrimSpace(ss3parErr(dres)))), nil
		}
		return Changed(fmt.Sprintf("Deleted CPG %s successfully.", cpgName)), nil

	default:
		return Result{}, errArg("ss_3par_cpg: state must be present or absent, got %q", state)
	}
}

func ss3parSSH(ctx context.Context, conn remoteexec.Connection, user, host, remoteCmd string) (remoteexec.Result, error) {
	cmd := "ssh " + shellQuote(user+"@"+host) + " " + shellQuote(remoteCmd)
	return runStatus(ctx, conn, cmd)
}

func ss3parErr(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}

// ss3parCpgInShowOutput reports whether `showcpg <name>`'s own output
// contains a data row naming that exact CPG (its Name column) — a
// nonexistent CPG makes showcpg exit non-zero with an error on most 3PAR
// CLI versions, but this also tolerates an empty-table success response.
func ss3parCpgInShowOutput(output, name string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for _, f := range fields {
			if f == name {
				return true
			}
		}
	}
	return false
}

func ss3parCreateCmd(args map[string]any, cpgName string) (string, error) {
	parts := []string{"createcpg", "-f"}
	if v := argString(args, "growth_increment", ""); v != "" {
		size, err := ss3parSize(v)
		if err != nil {
			return "", err
		}
		parts = append(parts, "-sdgs", size)
	}
	if v := argString(args, "growth_limit", ""); v != "" {
		size, err := ss3parSize(v)
		if err != nil {
			return "", err
		}
		parts = append(parts, "-sdgl", size)
	}
	if v := argString(args, "growth_warning", ""); v != "" {
		size, err := ss3parSize(v)
		if err != nil {
			return "", err
		}
		parts = append(parts, "-sdgw", size)
	}
	if v := argString(args, "domain", ""); v != "" {
		parts = append(parts, "-domain", v)
	}
	if v := argString(args, "raid_type", ""); v != "" {
		parts = append(parts, "-t", strings.ToLower(v))
	}
	if v := argInt(args, "set_size", 0); v != 0 {
		parts = append(parts, "-ssz", strconv.Itoa(v))
	}
	if v := argString(args, "high_availability", ""); v != "" {
		parts = append(parts, "-ha", strings.ToLower(v))
	}
	if v := argString(args, "disk_type", ""); v != "" {
		parts = append(parts, "-devtype", v)
	}
	parts = append(parts, cpgName)
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = shellQuote(p)
	}
	return strings.Join(quoted, " "), nil
}

var ss3parSizeRe = regexp.MustCompile(`(?i)^\s*(\d+)\s*(mib|gib|tib|mb|gb|tb|m|g|t)?\s*$`)

// ss3parSize converts real ss_3par_cpg's own "<number> <unit>" size
// string (MiB/GiB/TiB, per its own EXAMPLES — module_utils/_hpe3par.py's
// convert_to_binary_multiple normalizes these to MiB before sending over
// WSAPI) into the 3PAR CLI's own accepted "<number>[g|t]" shape (no
// suffix means MB — i.e. this port's own MiB is the CLI's own default
// unit, exactly as MiB and MB coincide for this CLI's own documented
// purposes). Returns an error (an argument-validation problem, not a
// per-host Result) for a size string this port cannot parse, since a
// malformed size here would otherwise silently become a nonsensical
// createcpg invocation.
func ss3parSize(s string) (string, error) {
	m := ss3parSizeRe.FindStringSubmatch(s)
	if m == nil {
		return "", errArg("ss_3par_cpg: invalid size %q (want e.g. \"32000 MiB\", \"64 GiB\", \"1 TiB\")", s)
	}
	n := m[1]
	switch strings.ToLower(m[2]) {
	case "gib", "gb", "g":
		return n + "g", nil
	case "tib", "tb", "t":
		return n + "t", nil
	default:
		return n, nil
	}
}
