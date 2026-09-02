package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDebconf implements (a subset of) Ansible's `debconf` module:
// pre-seeds a debconf question/answer via `debconf-set-selections`.
//
// Args: name (string, required) — the package name; question (string,
// required) — the debconf question key; value (string, required for
// state "present"); vtype (string, default "string" — one of string,
// boolean, select, multiselect, password, note); state (present|absent,
// default "present").
//
// Real ansible.builtin.debconf has no `state` argument at all — setting
// a debconf answer is the module's only operation, and there is no
// supported way to "unset" one (debconf keeps whatever was last set
// until something else overwrites it). This port accepts state for
// shape-symmetry with the other modules in this batch, but treats
// "absent" as a documented no-op (returns Ok without touching the
// target) rather than inventing a removal mechanism real debconf
// doesn't have.
//
// Idempotency: this port makes a best-effort check via `debconf-show
// <name>`, grepping for a "<question>: <value>" substring. debconf-show's
// output format varies by vtype (multiselect values are comma-joined,
// boolean/select values are the raw stored token, and each line is
// marked seen "*" or unseen " ") and this check does not parse any of
// that — it is a plain substring grep, so it can false-negative (report
// changed when the value was already set in a different textual form).
// Real ansible.builtin.debconf has essentially the same limitation:
// robustly parsing debconf-show requires understanding each vtype's own
// serialization, which isn't cheap to do from shell. Where this check
// can't be trusted, unconditionally (re-)setting the selection is a
// strictly safer failure mode than wrongly skipping a needed change.
func moduleDebconf(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	question, err := requireString(args, "question")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state == "absent" {
		return Ok("debconf: state=absent is a no-op (real debconf has no way to unset a selection)"), nil
	}
	if state != "present" {
		return Result{}, errArg("debconf: state must be present or absent, got %q", state)
	}
	value, err := requireString(args, "value")
	if err != nil {
		return Result{}, err
	}
	vtype := argString(args, "vtype", "string")

	already, err := debconfAlreadySet(ctx, conn, name, question, value)
	if err != nil {
		return Result{}, err
	}
	if already {
		return Ok(fmt.Sprintf("%s/%s already set", name, question)), nil
	}

	line := fmt.Sprintf("%s %s %s %s", name, question, vtype, value)
	cmd := "echo " + shellQuote(line) + " | debconf-set-selections"
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed(fmt.Sprintf("%s/%s set", name, question)), nil
}

// debconfAlreadySet makes a best-effort check for whether name/question
// is already set to value (see moduleDebconf's doc comment for its
// limitations).
func debconfAlreadySet(ctx context.Context, conn remoteexec.Connection, name, question, value string) (bool, error) {
	cmd := "debconf-show " + shellQuote(name) + " 2>/dev/null | grep -qF " + shellQuote(question+": "+value)
	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return false, fmt.Errorf("checking debconf selection for %s: %w", name, err)
	}
	return res.RC == 0, nil
}
