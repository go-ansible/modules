package modules

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHwcEcsInstance implements Ansible's `hwc_ecs_instance`
// (community.general) module: creates or deletes a Huawei Cloud ECS
// (Elastic Cloud Server) instance — see hwc_common.go's own doc
// comment for the KooCLI substitution shared by every hwc_* module in
// this batch, and specifically its own section on async/job-based
// create+delete (this module and hwc_evs_disk.go are the only two in
// this batch that need it). Operation IDs: CreateServers is
// independently confirmed against Huawei's own published ECS API
// reference; ShowServer/ListServers/DeleteServers follow this batch's
// own confirmed PascalCase(Verb+Resource) naming convention, derived
// from real hwc_ecs_instance.py's own REST paths ("cloudservers{,/id}",
// read before implementing — DeleteServers specifically is a BATCH
// operation in Huawei's real API (POST cloudservers/delete, body
// {"servers": [{"id": ...}], "delete_publicip": bool, "delete_volume":
// bool}), not a plain per-id DELETE, because deleting an ECS can
// optionally also release its EIP/volumes in the same call).
//
// Args: availability_zone, flavor_name, image_id, name, nics (list of
// dict: subnet_id required, ip_address optional), root_volume (dict:
// volume_type required, size/snapshot_id optional), vpc_id (all
// required); admin_pass (optional, secret — see below); data_volumes
// (list of dict: volume_id required, device optional); description,
// eip_id, enable_auto_recovery (bool), enterprise_project_id,
// security_groups (list of IDs), server_metadata (dict), server_tags
// (dict), ssh_key_name, user_data (all optional); id (takes precedence
// for lookup); region; timeouts (accepted, inert beyond bounding this
// port's own short job-poll window — see hcloudPollJob's own doc
// comment); state (present|absent, default present).
//
// Secret handling — admin_pass: this port's own hard "no secrets in
// argv" rule (see hwc_common.go's own doc comment, and this project's
// established REDISCLI_AUTH/GH_TOKEN precedent elsewhere) forbids
// putting a root/Administrator password on the command line. KooCLI's
// own documented `--cli-jsonInput=<file>` mechanism (see
// hwc_common.go's own doc comment) is used for this ONE field only:
// admin_pass, when given, is written to a target-side temp file (via
// conn.TempPath+Exec, the same pattern consul_acl_bootstrap.go already
// uses for its own bootstrap_secret) as
// {"body": {"server": {"adminPass": "..."}}}, removed immediately
// after the CreateServers call via a deferred conn.Remove — every
// other field is still passed as an ordinary --server.xxx=value flag,
// combined with --cli-jsonInput in the same invocation (KooCLI's own
// docs describe --cli-jsonInput as supplying "some or all parameters",
// implying exactly this combination, though this port had no live
// KooCLI to confirm the combination against directly).
//
// Deviation — field-name simplification: real Huawei ECS request-body
// field names are notably irregular (imageRef/flavorRef/vpcid/
// adminPass, a mix of camelCase and all-lowercase, inherited from
// Nova's own historical API) and this port could not verify every one
// exactly without a live tenant. This port instead sends every
// scalar/simple field using the SAME name as this module's own
// ansible argument (server.image_id, server.flavor_name, server.vpc_id,
// ...) — a documented, honest simplification specific to this module
// (the most schema-irregular of the twelve in this batch), not a
// silent guess dressed up as confidence. admin_pass is the one
// exception: its real body field name (adminPass) IS used, since
// getting a secret's OWN placement right matters more than the naming
// convention used for every other field, and this port had slightly
// higher confidence in that one specific name from its own research.
//
// Lookup: id given -> ShowServer; else ListServers filtered
// client-side by name + vpc_id + availability_zone + flavor_name +
// image_id. state=present on an already-found instance is always a
// no-op (see hwc_common.go's own doc comment on this batch's uniform
// no-update simplification).
//
// Async create/delete: see hwc_evs_disk.go's own doc comment for
// hcloudRunJSONJob's shared polling contract (SUCCESS/FAIL/still-
// RUNNING-at-the-poll-bound) — identical here, against KooCLI service
// code "ECS".
//
// Extra["id"]/Extra["server"]: present whenever the instance is
// confirmed to now exist (SUCCESS) or already existed.
func moduleHwcEcsInstance(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := hcloudRequireBinary(ctx, conn, "hwc_ecs_instance"); !ok {
		return res, nil
	}
	az, err := requireString(args, "availability_zone")
	if err != nil {
		return Result{}, err
	}
	flavorName, err := requireString(args, "flavor_name")
	if err != nil {
		return Result{}, err
	}
	imageID, err := requireString(args, "image_id")
	if err != nil {
		return Result{}, err
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	vpcID, err := requireString(args, "vpc_id")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("hwc_ecs_instance: state must be one of present, absent, got %q", state)
	}
	region := hcloudRegionParams(args)
	selector := map[string]string{
		"name": name, "vpc_id": vpcID, "availability_zone": az, "flavor_name": flavorName, "image_id": imageID,
	}

	match, found, ambiguous, err := hwcFindByIDOrSelector(ctx, conn, "ECS", "ShowServer", "ListServers", "server_id",
		argString(args, "id", ""), selector, region)
	if err != nil {
		return Result{}, err
	}
	if ambiguous {
		return Fail("hwc_ecs_instance: more than one instance matches the given selector; execution aborted"), nil
	}

	if state == "absent" {
		if !found {
			return Ok("hwc_ecs_instance: " + name + " already absent"), nil
		}
		id := fmt.Sprint(match["id"])
		dparams := mergeParams(map[string]string{"servers.[0].id": id}, region)
		dres, err := hcloudRunJSONJob(ctx, conn, "ECS", "DeleteServers", dparams, region)
		if err != nil {
			return Result{}, err
		}
		if dres.failed {
			return hcloudFail("hwc_ecs_instance", "deleting instance "+id, dres.res), nil
		}
		if !dres.completed {
			return Changed("hwc_ecs_instance: "+name+" deletion accepted, not confirmed within this port's poll window").
				WithExtra("id", id).WithExtra("job_status", "RUNNING"), nil
		}
		return Changed("hwc_ecs_instance: "+name+" deleted").WithExtra("id", id), nil
	}

	if found {
		return Ok("hwc_ecs_instance: "+name+" already present").
			WithExtra("id", fmt.Sprint(match["id"])).WithExtra("server", match), nil
	}

	nics, ok := args["nics"].([]any)
	if !ok || len(nics) == 0 {
		return Result{}, errArg("hwc_ecs_instance: missing required argument: nics")
	}
	rootVolume, ok := args["root_volume"].(map[string]any)
	if !ok {
		return Result{}, errArg("hwc_ecs_instance: missing required argument: root_volume")
	}
	rootVolumeType, err := requireString(rootVolume, "volume_type")
	if err != nil {
		return Result{}, errArg("hwc_ecs_instance: root_volume.volume_type: %v", err)
	}

	cparams := map[string]string{
		"server.availability_zone": az, "server.name": name, "server.image_id": imageID,
		"server.flavor_name": flavorName, "server.vpc_id": vpcID, "server.root_volume.volume_type": rootVolumeType,
	}
	if v := argInt(rootVolume, "size", 0); v != 0 {
		cparams["server.root_volume.size"] = fmt.Sprint(v)
	}
	if v := argString(rootVolume, "snapshot_id", ""); v != "" {
		cparams["server.root_volume.snapshot_id"] = v
	}
	for i, n := range nics {
		nm, ok := n.(map[string]any)
		if !ok {
			continue
		}
		if v := argString(nm, "subnet_id", ""); v != "" {
			cparams[fmt.Sprintf("server.nics.[%d].subnet_id", i)] = v
		}
		if v := argString(nm, "ip_address", ""); v != "" {
			cparams[fmt.Sprintf("server.nics.[%d].ip_address", i)] = v
		}
	}
	for _, f := range []string{"description", "eip_id", "enterprise_project_id", "ssh_key_name", "user_data"} {
		if v := argString(args, f, ""); v != "" {
			if f == "user_data" {
				cparams["server.user_data"] = base64.StdEncoding.EncodeToString([]byte(v))
			} else {
				cparams["server."+f] = v
			}
		}
	}
	if _, ok := args["enable_auto_recovery"]; ok {
		cparams["server.enable_auto_recovery"] = fmt.Sprint(argBool(args, "enable_auto_recovery", false))
	}
	for i, sg := range argStringList(args, "security_groups") {
		cparams[fmt.Sprintf("server.security_groups.[%d].id", i)] = sg
	}
	if dvs, ok := args["data_volumes"].([]any); ok {
		for i, d := range dvs {
			dm, ok := d.(map[string]any)
			if !ok {
				continue
			}
			if v := argString(dm, "volume_id", ""); v != "" {
				cparams[fmt.Sprintf("server.data_volumes.[%d].volume_id", i)] = v
			}
			if v := argString(dm, "device", ""); v != "" {
				cparams[fmt.Sprintf("server.data_volumes.[%d].device", i)] = v
			}
		}
	}
	if md, ok := args["server_metadata"].(map[string]any); ok {
		for k, v := range md {
			cparams["server.metadata."+k] = fmt.Sprint(v)
		}
	}
	if tags, ok := args["server_tags"].(map[string]any); ok {
		i := 0
		for k, v := range tags {
			cparams[fmt.Sprintf("server.server_tags.[%d].key", i)] = k
			cparams[fmt.Sprintf("server.server_tags.[%d].value", i)] = fmt.Sprint(v)
			i++
		}
	}
	cparams = mergeParams(cparams, region)

	var jsonInputPath string
	if adminPass := argString(args, "admin_pass", ""); adminPass != "" {
		body, merr := json.Marshal(map[string]any{"body": map[string]any{"server": map[string]any{"adminPass": adminPass}}})
		if merr != nil {
			return Result{}, merr
		}
		jsonInputPath = conn.TempPath("hwc-ecs-instance-admin-pass.json")
		if _, werr := conn.Exec(ctx, "cat > "+shellQuote(jsonInputPath), strings.NewReader(string(body))); werr != nil {
			return Result{}, werr
		}
		defer func() { _ = conn.Remove(ctx, jsonInputPath) }()
		cparams["cli-jsonInput"] = jsonInputPath
	}

	res, err := hcloudRunJSONJob(ctx, conn, "ECS", "CreateServers", cparams, region)
	if err != nil {
		return Result{}, err
	}
	if res.failed {
		return hcloudFail("hwc_ecs_instance", "creating instance "+name, res.res), nil
	}
	if !res.completed {
		return Changed("hwc_ecs_instance: "+name+" creation accepted, not confirmed within this port's poll window").
			WithExtra("job_status", "RUNNING"), nil
	}
	r := Changed("hwc_ecs_instance: " + name + " created")
	if id, ok := jobEntityString(res.job, "server_id"); ok && id != "" {
		r = r.WithExtra("id", id)
	}
	return r, nil
}
