package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// Real OpenNebula host STATE numeric codes, verified against
// OpenNebula's own Host States Reference documentation (not guessed):
// INIT=0, MONITORING_MONITORED=1, MONITORED=2, ERROR=3, DISABLED=4,
// MONITORING_ERROR=5, MONITORING_INIT=6, MONITORING_DISABLED=7,
// OFFLINE=8.
const (
	oneHostStateMonitored       = 2
	oneHostStateError           = 3
	oneHostStateDisabled        = 4
	oneHostStateMonitoringError = 5
	oneHostStateOffline         = 8
	oneHostAbsent               = -99 // this port's own sentinel, matching real one_host.py's own HOST_ABSENT
)

// moduleOneHost implements Ansible's `one_host` module: manages an
// OpenNebula host's lifecycle state, cluster membership, and template,
// via the `onehost`/`onecluster` CLIs (see one_common.go's own doc
// comment for the pyone-vs-CLI substitution and the ONE_XMLRPC/ONE_AUTH
// auth story).
//
// Args: name (required); state (present|absent|enabled|disabled|
// offline, default "present"); im_mad_name/vmm_mad_name (default
// "kvm" each); cluster_id (int, default 0) — mutually exclusive with
// cluster_name (an error if BOTH are explicitly given, matching real
// one_host's own mutually_exclusive declaration); cluster_name
// (string, resolved to a numeric cluster_id via `onecluster list -x`,
// matching real one_host's own resolve_parameters); labels ([]string);
// template (dict, aliased attributes) — merged onto the host's own
// TEMPLATE. wait_timeout is NOT a real one_host argument (real
// one_host has no wait_timeout of its own — it always polls with an
// internal default); this port accepts no such argument here and does
// not poll for a target state to be reached after enable/disable/
// offline/create at all (unlike one_vm.go's own documented `wait` gap,
// this isn't even a real argument to skip — real one_host's own
// wait_for_host_state has no argument-level knob, it's baked into
// every transition; this port simply performs the one state-changing
// `onehost` call and reports Changed without confirming convergence,
// an honestly narrower but still real gap).
//
// State transitions faithfully match real one_host.py's own run()
// exactly, including which "wrong state" combinations it refuses with
// a hard failure rather than silently reinterpreting (e.g.
// state=disabled on an absent host fails outright, it does not create
// then disable).
//
// Template/cluster reconciliation, when state != absent: real
// one_host's own requires_template_update casts each desired value
// (a list joined with ", ", anything else stringified) and compares it
// against the host's OWN current TEMPLATE key-for-key; ANY missing or
// differing key triggers a single `onehost update <id> --append -`
// (extra "KEY = \"value\"\n" lines piped over stdin) covering the
// whole desired set at once — this port reproduces that same
// cast-and-compare logic, then, separately, moves the host to
// cluster_id via `onecluster addhost <cluster_id> <hostid>` if it
// differs from the host's own current CLUSTER_ID.
//
// This port declares no Extra fields, matching real one_host's own
// empty `RETURN = """ """` block (marked in its own source as
// "pending setting guidelines on returned values").
func moduleOneHost(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent", "enabled", "disabled", "offline":
	default:
		return Result{}, errArg("one_host: state must be one of present, absent, enabled, disabled, offline, got %q", state)
	}
	_, hasClusterID := args["cluster_id"]
	_, hasClusterName := args["cluster_name"]
	if hasClusterID && hasClusterName {
		return Result{}, errArg("one_host: cluster_id and cluster_name are mutually exclusive")
	}
	imMad := argString(args, "im_mad_name", "kvm")
	vmmMad := argString(args, "vmm_mad_name", "kvm")
	url := oneAuth(args)
	if res, ok := oneRequireBinary(ctx, conn, "onehost", "one_host"); !ok {
		return res, nil
	}

	clusterID := argInt(args, "cluster_id", 0)
	if clusterName := argString(args, "cluster_name", ""); clusterName != "" {
		if res, ok := oneRequireBinary(ctx, conn, "onecluster", "one_host"); !ok {
			return res, nil
		}
		pool, err := oneListXML(ctx, conn, url, "onecluster")
		if err != nil {
			return Result{}, err
		}
		item, ok := oneResolveByName(pool, "CLUSTER", clusterName)
		if !ok {
			return Fail("one_host: cluster " + clusterName + " not found"), nil
		}
		clusterID = item.childInt("ID")
	}

	pool, err := oneListXML(ctx, conn, url, "onehost")
	if err != nil {
		return Result{}, err
	}
	host, found := oneResolveByName(pool, "HOST", name)
	currentState := oneHostAbsent
	if found {
		currentState = host.childInt("STATE")
	}

	changed := false
	allocate := func() (Result, bool) {
		res, err := oneRun(ctx, conn, url, "onehost", "create", name, "-i", imMad, "-v", vmmMad, "--cluster", strconv.Itoa(clusterID))
		if err != nil {
			return Result{}, false
		}
		if res.RC != 0 {
			return Fail("one_host: could not allocate host: " + oneErrMsg(res)), false
		}
		changed = true
		return Result{}, true
	}

	switch state {
	case "present":
		if currentState == oneHostAbsent {
			if r, ok := allocate(); !ok {
				return r, nil
			}
		} else if currentState == oneHostStateError || currentState == oneHostStateMonitoringError {
			return Fail("one_host: invalid host state"), nil
		}

	case "enabled":
		switch {
		case currentState == oneHostAbsent:
			if r, ok := allocate(); !ok {
				return r, nil
			}
		case currentState == oneHostStateDisabled || currentState == oneHostStateOffline:
			if res, err := oneRun(ctx, conn, url, "onehost", "enable", host.childText("ID")); err != nil {
				return Result{}, err
			} else if res.RC != 0 {
				return Fail("one_host: could not enable host: " + oneErrMsg(res)), nil
			}
			changed = true
		case currentState == oneHostStateMonitored:
			// already enabled — no-op
		default:
			return Fail("one_host: unknown host state, cowardly refusing to change state to enable"), nil
		}

	case "disabled":
		switch {
		case currentState == oneHostAbsent:
			return Fail("one_host: absent host cannot be put in disabled state"), nil
		case currentState == oneHostStateMonitored || currentState == oneHostStateOffline:
			if res, err := oneRun(ctx, conn, url, "onehost", "disable", host.childText("ID")); err != nil {
				return Result{}, err
			} else if res.RC != 0 {
				return Fail("one_host: could not disable host: " + oneErrMsg(res)), nil
			}
			changed = true
		case currentState == oneHostStateDisabled:
			// already disabled — no-op
		default:
			return Fail("one_host: unknown host state, cowardly refusing to change state to disable"), nil
		}

	case "offline":
		switch {
		case currentState == oneHostAbsent:
			return Fail("one_host: absent host cannot be placed in offline state"), nil
		case currentState == oneHostStateMonitored || currentState == oneHostStateDisabled:
			if res, err := oneRun(ctx, conn, url, "onehost", "offline", host.childText("ID")); err != nil {
				return Result{}, err
			} else if res.RC != 0 {
				return Fail("one_host: could not set host offline: " + oneErrMsg(res)), nil
			}
			changed = true
		case currentState == oneHostStateOffline:
			// already offline — no-op
		default:
			return Fail("one_host: unknown host state, cowardly refusing to change state to offline"), nil
		}

	case "absent":
		if currentState != oneHostAbsent {
			if res, err := oneRun(ctx, conn, url, "onehost", "delete", host.childText("ID")); err != nil {
				return Result{}, err
			} else if res.RC != 0 {
				return Fail("one_host: could not delete host from cluster: " + oneErrMsg(res)), nil
			}
			changed = true
		}
	}

	if state != "absent" {
		hostID := host.childText("ID")
		if hostID == "" {
			// just allocated — resolve the freshly-created host's ID.
			pool, err := oneListXML(ctx, conn, url, "onehost")
			if err != nil {
				return Result{}, err
			}
			item, ok := oneResolveByName(pool, "HOST", name)
			if !ok {
				return Fail("one_host: host was allocated but could not be found afterwards"), nil
			}
			host = item
			hostID = host.childText("ID")
		}

		desired := map[string]any{}
		if tmpl, ok := args["template"].(map[string]any); ok {
			for k, v := range tmpl {
				desired[k] = v
			}
		} else if attrs, ok := args["attributes"].(map[string]any); ok {
			for k, v := range attrs {
				desired[k] = v
			}
		}
		if labels := argStringList(args, "labels"); labels != nil {
			desired["LABELS"] = labels
		}

		templateNode, _ := host.child("TEMPLATE")
		if oneTemplateNeedsUpdate(templateNode, desired) {
			body := oneRenderTemplate(desired)
			res, err := oneRunStdin(ctx, conn, url, "onehost", body, "update", hostID, "-", "--append")
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("one_host: failed to update the host template: " + oneErrMsg(res)), nil
			}
			changed = true
		}

		currentClusterID := host.childInt("CLUSTER_ID")
		if clusterID != currentClusterID {
			if res, ok := oneRequireBinary(ctx, conn, "onecluster", "one_host"); !ok {
				return res, nil
			}
			res, err := oneRun(ctx, conn, url, "onecluster", "addhost", strconv.Itoa(clusterID), hostID)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("one_host: failed to update the host cluster: " + oneErrMsg(res)), nil
			}
			changed = true
		}
	}

	return Result{Changed: changed}, nil
}

// oneCastTemplateValue mirrors real one_host.py's own cast_template: a
// list is joined with ", "; anything else is stringified.
func oneCastTemplateValue(v any) string {
	switch x := v.(type) {
	case []string:
		return strings.Join(x, ", ")
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = fmtAny(e)
		}
		return strings.Join(parts, ", ")
	case string:
		return x
	default:
		return fmtAny(x)
	}
}

// oneTemplateNeedsUpdate mirrors real one_host.py's own
// requires_template_update: false if desired is empty; else true if
// any desired key is missing from current or differs once cast.
func oneTemplateNeedsUpdate(current oneXMLNode, desired map[string]any) bool {
	if len(desired) == 0 {
		return false
	}
	for k, v := range desired {
		want := oneCastTemplateValue(v)
		have, ok := current.child(k)
		if !ok || have.text() != want {
			return true
		}
	}
	return false
}

// oneRenderTemplate renders desired as OpenNebula template text
// ("KEY = \"value\"" lines, one per key), for piping to `<bin> update
// <id> - --append`.
func oneRenderTemplate(desired map[string]any) string {
	var b strings.Builder
	for k, v := range desired {
		b.WriteString(k)
		b.WriteString(` = "`)
		b.WriteString(strings.ReplaceAll(oneCastTemplateValue(v), `"`, `\"`))
		b.WriteString("\"\n")
	}
	return b.String()
}
