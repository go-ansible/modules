package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleJenkinsNode implements Ansible's `jenkins_node`
// (community.general) module: creates, deletes, or changes the
// online/offline state of a Jenkins agent (node) — see
// jenkins_common.go's own doc comment for the jenkins-cli.jar
// substitution shared by this batch's other REST/CLI-facing jenkins_*
// modules. Commands used: `create-node`/`delete-node` (create/delete a
// node from an XML config piped over stdin — confirmed real, standard
// jenkins-cli built-ins), `online-node`/`offline-node` (confirmed real
// — "resumes/stops using a node for performing builds"). `get-node`
// itself is NOT a confirmed built-in this port could find documented
// evidence for; this port instead uses `dump-node-config <name>`
// (confirmed real: "dumps the node definition XML to stdout") for both
// existence-probing (a non-zero exit means the node doesn't exist) and
// reading a node's current config to patch when updating.
//
// Deviation — no confirmed update-node command: unlike create-job's
// own confirmed update-job counterpart, this port could not confirm
// jenkins-cli ships an update-node command in every version (some
// Jenkins CLI history shows it added later than create-node/
// delete-node) — see this file's own research notes. This port
// attempts `update-node <name>` (piping a patched copy of the node's
// own current dump-node-config XML, with <numExecutors>/<label>
// replaced) whenever labels/num_executors are given on an EXISTING
// node; if the target's own jenkins-cli genuinely lacks that command,
// the call fails loud with jenkins-cli's own "is not a Jenkins
// command" error rather than silently doing nothing — an honest,
// bounded risk this port accepts rather than inventing a command name
// with no confirmation at all it could not even attempt.
//
// Args: name (required); labels (list of strings, optional); num_executors
// (int, optional); offline_message (string — only valid when state=disabled,
// matching real jenkins_node.py's own documented constraint: "If
// offline_message is given and requested state is not disabled, an
// error is raised"); user, password, token; url; state
// (enabled|disabled|present|absent, default present).
//
// present: create the node (a minimal Dumb Slave XML: numExecutors
// default 1, mode NORMAL, a JNLPLauncher — the only launcher shape
// this port can construct without also driving an SSH-credential setup
// it has no CLI primitive for) if it doesn't exist; if it does and
// labels/num_executors were given, patch and update-node (see
// deviation above). absent: delete-node if present, else no-op.
// enabled: online-node. disabled: offline-node, with -m
// <offline_message> when given.
//
// Extra: created/deleted/enabled/disabled/configured (bool, matching
// real jenkins_node.py's own documented return field NAMES, though
// this port sets at most one true per run rather than that module's
// own finer-grained combination tracking); name; state; url; user.
func moduleJenkinsNode(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := jenkinsRequireRuntime(ctx, conn, "jenkins_node"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	url := argString(args, "url", "http://localhost:8080")
	user, password, token := jenkinsAuthArgs(args, "user")
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent", "enabled", "disabled":
	default:
		return Result{}, errArg("jenkins_node: state must be one of enabled, disabled, present, absent, got %q", state)
	}
	offlineMessage := argString(args, "offline_message", "")
	if offlineMessage != "" && state != "disabled" {
		return Result{}, errArg("jenkins_node: offline_message requires state=disabled")
	}

	probe, err := jenkinsRun(ctx, conn, url, user, password, token, nil, "dump-node-config", name)
	if err != nil {
		return Result{}, err
	}
	found := probe.RC == 0

	extra := func(r Result) Result {
		return r.WithExtra("name", name).WithExtra("state", state).WithExtra("url", url).WithExtra("user", user)
	}

	if state == "absent" {
		if !found {
			return extra(Ok("jenkins_node: "+name+" already absent")).WithExtra("deleted", false), nil
		}
		dres, err := jenkinsRun(ctx, conn, url, user, password, token, nil, "delete-node", name)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return Fail("jenkins_node: unable to delete " + name + ": " + jenkinsErrMsg(dres)), nil
		}
		return extra(Changed("jenkins_node: "+name+" deleted")).WithExtra("deleted", true), nil
	}

	if state == "enabled" || state == "disabled" {
		if !found {
			return Fail("jenkins_node: " + name + " does not exist, cannot change online state"), nil
		}
		cmd := "online-node"
		argv := []string{cmd, name}
		if state == "disabled" {
			argv = []string{"offline-node", name}
			if offlineMessage != "" {
				argv = append(argv, "-m", offlineMessage)
			}
		}
		ores, err := jenkinsRun(ctx, conn, url, user, password, token, nil, argv...)
		if err != nil {
			return Result{}, err
		}
		if ores.RC != 0 {
			return Fail("jenkins_node: unable to set " + name + " " + state + ": " + jenkinsErrMsg(ores)), nil
		}
		key := "enabled"
		if state == "disabled" {
			key = "disabled"
		}
		return extra(Changed("jenkins_node: "+name+" "+state)).WithExtra(key, true), nil
	}

	// state=present
	labels := argStringList(args, "labels")
	_, hasNumExecutors := args["num_executors"]
	numExecutors := argInt(args, "num_executors", 1)

	if !found {
		xml := jenkinsNodeCreateXML(name, numExecutors, labels)
		cres, err := jenkinsRun(ctx, conn, url, user, password, token, strings.NewReader(xml), "create-node", name)
		if err != nil {
			return Result{}, err
		}
		if cres.RC != 0 {
			return Fail("jenkins_node: unable to create " + name + ": " + jenkinsErrMsg(cres)), nil
		}
		return extra(Changed("jenkins_node: "+name+" created")).WithExtra("created", true), nil
	}

	if len(labels) == 0 && !hasNumExecutors {
		return extra(Ok("jenkins_node: " + name + " already present")), nil
	}
	patched := jenkinsNodePatchXML(probe.Stdout, numExecutors, hasNumExecutors, labels)
	ures, err := jenkinsRun(ctx, conn, url, user, password, token, strings.NewReader(patched), "update-node", name)
	if err != nil {
		return Result{}, err
	}
	if ures.RC != 0 {
		return Fail("jenkins_node: unable to update " + name + ": " + jenkinsErrMsg(ures)), nil
	}
	return extra(Changed("jenkins_node: "+name+" configured")).WithExtra("configured", true), nil
}

// jenkinsNodeCreateXML renders a minimal Jenkins "slave" (dumb slave,
// JNLP launcher) config XML suitable for `create-node`'s own stdin —
// the simplest launcher shape this port can construct without also
// needing an SSH host/credential this module's own real argument_spec
// has no field for.
func jenkinsNodeCreateXML(name string, numExecutors int, labels []string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<slave>
  <name>%s</name>
  <description></description>
  <remoteFS>/home/jenkins</remoteFS>
  <numExecutors>%d</numExecutors>
  <mode>NORMAL</mode>
  <launcher class="hudson.slaves.JNLPLauncher"/>
  <label>%s</label>
  <nodeProperties/>
</slave>
`, xmlEscape(name), numExecutors, xmlEscape(strings.Join(labels, " ")))
}

// jenkinsNodePatchXML replaces current's own <numExecutors>/<label>
// element bodies (a plain string replace over the whole document — a
// deliberately simple approach, since this port has no XML library in
// its own dependency graph to parse and re-serialize a full node
// config XML faithfully; see moduleJenkinsNode's own doc comment).
func jenkinsNodePatchXML(current string, numExecutors int, setNumExecutors bool, labels []string) string {
	out := current
	if setNumExecutors {
		out = xmlReplaceElement(out, "numExecutors", strconv.Itoa(numExecutors))
	}
	if len(labels) > 0 {
		out = xmlReplaceElement(out, "label", xmlEscape(strings.Join(labels, " ")))
	}
	return out
}

// xmlReplaceElement replaces the text content of the first
// <tag>...</tag> element in doc with value, leaving every other
// element untouched — the narrow XML-editing primitive
// jenkinsNodePatchXML needs (see its own doc comment for why this port
// doesn't use a full XML library here).
func xmlReplaceElement(doc, tag, value string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(doc, open)
	if start < 0 {
		return doc
	}
	end := strings.Index(doc[start:], close)
	if end < 0 {
		return doc
	}
	end += start
	return doc[:start+len(open)] + value + doc[end:]
}
