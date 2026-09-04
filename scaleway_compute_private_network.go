package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayComputePrivateNetwork implements Ansible's
// `scaleway_compute_private_network` (community.general) module:
// attaches or detaches a Scaleway Instance to/from a Private Network,
// via `scw instance private-nic create/list/delete` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI for real scaleway_compute_private_network's own direct
// REST API calls, and for the auth/wait deviations shared by every
// scaleway_* module in this batch.
//
// Args: compute_id (required) — the server's ID; private_network_id
// (required); region (required — real scaleway_compute_private_network's
// own `region` argument is INSTANCE-API-zone-shaped, same as
// scaleway_compute's own, not the container/function-family
// fr-par/nl-ams/pl-waw shape — see scaleway_common.go's own doc comment
// on scwZone for the translation this port applies); project (required)
// — accepted for argument-shape compatibility but has NO EFFECT: real
// scaleway_compute_private_network's own core() declares it required in
// argument_spec but never actually reads module.params["project"]
// anywhere in its own present_strategy/absent_strategy (verified
// directly against scaleway_compute_private_network.py — this is real
// scaleway_compute_private_network's own dead argument, not a gap this
// port introduces); state (present|absent, default present).
//
// present/absent: lists the server's own private NICs (`scw instance
// private-nic list server-id=... zone=...`) and looks for one whose
// private_network_id matches — matching real get_nics_info's own linear
// scan exactly (not a name-keyed lookup, so this module does not use
// scwFindByName). present creates one (`scw instance private-nic create
// server-id=... private-network-id=... zone=...`) if none matches, else
// no-op. absent deletes the matching one (`scw instance private-nic
// delete server-id=... private-nic-id=... zone=...`) if found, else
// no-op.
//
// Extra["scaleway_compute_private_network"]: matches real
// scaleway_compute_private_network's own identically-named return key.
// Deviation — real scaleway_compute_private_network.py's own RETURN
// VALUES doc sample shows private-NETWORK-shaped fields (name/
// organization_id/tags/zone), but its own present_strategy/
// absent_strategy code returns response.json straight from the
// servers/{id}/private_nics POST/DELETE endpoint — the private_NIC
// object (id/server_id/private_network_id/mac_address/state/tags), not
// the network. This port follows the verified CODE, not the stale doc
// sample, per this project's own "read the reference before
// implementing" rule — Extra here is `scw`'s own private-nic JSON
// object. On state=absent with a nic found, this port returns the nic
// AS IT WAS FOUND (before deletion) rather than the DELETE call's own
// (typically empty) response body, since real
// scaleway_compute_private_network's own response.json from a delete
// is not usable output either — a disclosed, harmless difference from
// an already-mostly-empty real return value.
func moduleScalewayComputePrivateNetwork(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_compute_private_network"); !ok {
		return res, nil
	}
	computeID, err := requireString(args, "compute_id")
	if err != nil {
		return Result{}, err
	}
	pnID, err := requireString(args, "private_network_id")
	if err != nil {
		return Result{}, err
	}
	if _, err := requireString(args, "project"); err != nil {
		return Result{}, err
	}
	region, err := requireString(args, "region")
	if err != nil {
		return Result{}, err
	}
	zone, err := scwZone(region)
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("scaleway_compute_private_network: state must be one of present, absent, got %q", state)
	}

	res, err := scwRunJSON(ctx, conn, "instance", "private-nic", "list", "server-id="+computeID, "zone="+zone)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("scaleway_compute_private_network: failed to list private NICs for server " + computeID + ": " + scwErrMsg(res)), nil
	}
	var nics []map[string]any
	if derr := scwDecode(res.Stdout, &nics); derr != nil {
		return Result{}, derr
	}
	var found map[string]any
	for _, n := range nics {
		if id, ok := n["private_network_id"].(string); ok && id == pnID {
			found = n
			break
		}
	}

	if state == "absent" {
		if found == nil {
			return Ok("").WithExtra("scaleway_compute_private_network", map[string]any{}), nil
		}
		nicID, _ := found["id"].(string)
		delRes, err := scwRun(ctx, conn, "instance", "private-nic", "delete", "server-id="+computeID,
			"private-nic-id="+nicID, "zone="+zone)
		if err != nil {
			return Result{}, err
		}
		if delRes.RC != 0 {
			return Fail("scaleway_compute_private_network: failed to detach private network " + pnID + " from server " + computeID + ": " + scwErrMsg(delRes)), nil
		}
		return Changed("").WithExtra("scaleway_compute_private_network", found), nil
	}

	if found != nil {
		return Ok("").WithExtra("scaleway_compute_private_network", found), nil
	}
	createRes, err := scwRunJSON(ctx, conn, "instance", "private-nic", "create", "server-id="+computeID,
		"private-network-id="+pnID, "zone="+zone)
	if err != nil {
		return Result{}, err
	}
	if createRes.RC != 0 {
		return Fail("scaleway_compute_private_network: failed to attach private network " + pnID + " to server " + computeID + ": " + scwErrMsg(createRes)), nil
	}
	var created map[string]any
	if derr := scwDecode(createRes.Stdout, &created); derr != nil {
		return Result{}, derr
	}
	return Changed("").WithExtra("scaleway_compute_private_network", created), nil
}
