package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIptables implements (a subset of) Ansible's `iptables` module:
// composes an `iptables` invocation from a handful of common match/
// target fields, checking whether the rule already exists via
// `iptables -C` before adding or removing it — the same idempotency
// approach real ansible.builtin.iptables itself uses (it also shells
// out to iptables and relies on -C for existence checking, rather than
// parsing `iptables-save` output).
//
// Args: chain (string, required); protocol (string, optional);
// source, destination (string, optional); destination_port (string,
// optional); jump (string, optional — e.g. ACCEPT, DROP, REJECT);
// state (present|absent, default "present"); action (append|insert,
// default "append" — only meaningful for state=present: append uses
// -A, insert uses -I); table (string, optional — passed as `-t table`
// when given).
//
// This port does not attempt real iptables' full flag surface: no
// ctstate, in/out interface, icmp-type, limit/comment matching, IPv6
// (ip6tables) support, or chain_management (auto-creating a
// user-defined chain). A reasonable common-case subset, documented as
// such — this is the same "cover the frequent case, fail cleanly
// rather than silently drop a flag" tradeoff apt_key.go and others in
// this batch make.
func moduleIptables(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	chain, err := requireString(args, "chain")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("iptables: state must be present or absent, got %q", state)
	}
	action := argString(args, "action", "append")
	if action != "append" && action != "insert" {
		return Result{}, errArg("iptables: action must be append or insert, got %q", action)
	}

	ruleArgs := iptablesRuleArgs(args)
	table := argString(args, "table", "")
	var tablePrefix string
	if table != "" {
		tablePrefix = "-t " + shellQuote(table) + " "
	}

	checkCmd := "iptables " + tablePrefix + "-C " + shellQuote(chain) + ruleArgs
	res, err := runStatus(ctx, conn, checkCmd)
	if err != nil {
		return Result{}, err
	}
	exists := res.RC == 0

	switch state {
	case "present":
		if exists {
			return Ok("rule already present"), nil
		}
		flag := "-A"
		if action == "insert" {
			flag = "-I"
		}
		cmd := "iptables " + tablePrefix + flag + " " + shellQuote(chain) + ruleArgs
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed("rule added to " + chain), nil

	default: // "absent"
		if !exists {
			return Ok("rule already absent"), nil
		}
		cmd := "iptables " + tablePrefix + "-D " + shellQuote(chain) + ruleArgs
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed("rule removed from " + chain), nil
	}
}

// iptablesRuleArgs builds the match/target flags shared by the -C
// (check), -A/-I (add), and -D (delete) forms of the iptables
// invocation, separated out so its exact shape can be asserted
// directly in tests. Each returned flag is preceded by a space, so the
// result can be appended directly after the chain name.
func iptablesRuleArgs(args map[string]any) string {
	var b strings.Builder
	if v := argString(args, "protocol", ""); v != "" {
		b.WriteString(" -p " + shellQuote(v))
	}
	if v := argString(args, "source", ""); v != "" {
		b.WriteString(" -s " + shellQuote(v))
	}
	if v := argString(args, "destination", ""); v != "" {
		b.WriteString(" -d " + shellQuote(v))
	}
	if v := argString(args, "destination_port", ""); v != "" {
		b.WriteString(" --dport " + shellQuote(v))
	}
	if v := argString(args, "jump", ""); v != "" {
		b.WriteString(" -j " + shellQuote(v))
	}
	return b.String()
}
