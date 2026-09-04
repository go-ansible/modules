package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHwcEvsDisk implements Ansible's `hwc_evs_disk`
// (community.general) module: creates or deletes a Huawei Cloud EVS
// (Elastic Volume Service) block-storage disk — see hwc_common.go's
// own doc comment for the KooCLI substitution shared by every hwc_*
// module in this batch, and specifically its own section on
// async/job-based create+delete, which this module and
// hwc_ecs_instance.go are the only two in this batch that need.
// Operation IDs (CreateVolume/ShowVolume/DeleteVolume/ListVolumes,
// KooCLI service code "EVS") are DERIVED from real hwc_evs_disk.py's
// own REST path ("cloudvolumes/{id}", the EVS v2.1 proprietary API,
// read before implementing — NOT the OpenStack-Cinder-compatible
// "volumes/{id}" path a different Huawei API also exposes), following
// hwc_common.go's own confirmed PascalCase(Verb+Resource) convention;
// DeleteVolume specifically was also independently confirmed against
// Huawei's own published EVS API reference during this batch's
// research.
//
// Args: availability_zone, name, volume_type (required); backup_id,
// description, enable_full_clone, enable_scsi, enable_share (bool),
// encryption_id, enterprise_project_id, image_id, size (int),
// snapshot_id (all optional); id (takes precedence for lookup);
// region; timeouts (accepted, inert beyond bounding this port's own
// short job-poll window — see hcloudPollJob's own doc comment for why
// this port does not wait up to the real default 30 minutes); state
// (present|absent, default present).
//
// Lookup: id given -> ShowVolume; else ListVolumes filtered
// client-side by availability_zone + name + volume_type.
// state=present on an already-found disk is always a no-op (see
// hwc_common.go's own doc comment on this batch's uniform no-update
// simplification).
//
// Async create/delete: CreateVolume/DeleteVolume return a job_id
// immediately; this module polls ShowJob (KooCLI service "EVS") via
// hcloudPollJob. A job that completes with job_status=SUCCESS reports
// Changed with the disk's own id (read from the job's own
// entities.volume_id on create, or the resource's own already-known id
// on delete); job_status=FAIL is a Fail with the job's own fail_reason;
// a job still RUNNING when hcloudPollJob's own bound is reached is
// still reported as Changed (the request was accepted server-side —
// this port's own inability to confirm completion within its bounded
// poll is not the same as the request having failed), with
// Extra["job_status"]="RUNNING" and a msg noting the poll bound was
// reached, so a caller inspecting the result can tell the difference
// from a confirmed SUCCESS.
//
// Extra["id"]/Extra["volume"]: present whenever the disk is confirmed
// to now exist (SUCCESS) or already existed.
func moduleHwcEvsDisk(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := hcloudRequireBinary(ctx, conn, "hwc_evs_disk"); !ok {
		return res, nil
	}
	az, err := requireString(args, "availability_zone")
	if err != nil {
		return Result{}, err
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	volumeType, err := requireString(args, "volume_type")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("hwc_evs_disk: state must be one of present, absent, got %q", state)
	}
	region := hcloudRegionParams(args)
	selector := map[string]string{"availability_zone": az, "name": name, "volume_type": volumeType}

	match, found, ambiguous, err := hwcFindByIDOrSelector(ctx, conn, "EVS", "ShowVolume", "ListVolumes", "volume_id",
		argString(args, "id", ""), selector, region)
	if err != nil {
		return Result{}, err
	}
	if ambiguous {
		return Fail(fmt.Sprintf("hwc_evs_disk: more than one disk matches availability_zone=%s name=%s volume_type=%s; execution aborted", az, name, volumeType)), nil
	}

	if state == "absent" {
		if !found {
			return Ok("hwc_evs_disk: " + name + " already absent"), nil
		}
		id := fmt.Sprint(match["id"])
		dres, err := hcloudRunJSONJob(ctx, conn, "EVS", "DeleteVolume", mergeParams(map[string]string{"volume_id": id}, region), region)
		if err != nil {
			return Result{}, err
		}
		if dres.failed {
			return hcloudFail("hwc_evs_disk", "deleting disk "+id, dres.res), nil
		}
		if !dres.completed {
			return Changed("hwc_evs_disk: "+name+" deletion accepted, not confirmed within this port's poll window").
				WithExtra("id", id).WithExtra("job_status", "RUNNING"), nil
		}
		return Changed("hwc_evs_disk: "+name+" deleted").WithExtra("id", id), nil
	}

	if found {
		return Ok("hwc_evs_disk: "+name+" already present").
			WithExtra("id", fmt.Sprint(match["id"])).WithExtra("volume", match), nil
	}

	cparams := map[string]string{
		"volume.availability_zone": az, "volume.name": name, "volume.volume_type": volumeType,
	}
	for _, f := range []string{"backup_id", "description", "encryption_id", "enterprise_project_id", "image_id", "snapshot_id"} {
		if v := argString(args, f, ""); v != "" {
			cparams["volume."+f] = v
		}
	}
	for _, f := range []string{"enable_full_clone", "enable_scsi", "enable_share"} {
		if _, ok := args[f]; ok {
			cparams["volume."+f] = fmt.Sprint(argBool(args, f, false))
		}
	}
	if v := argInt(args, "size", 0); v != 0 {
		cparams["volume.size"] = fmt.Sprint(v)
	}
	cparams = mergeParams(cparams, region)

	res, err := hcloudRunJSONJob(ctx, conn, "EVS", "CreateVolume", cparams, region)
	if err != nil {
		return Result{}, err
	}
	if res.failed {
		return hcloudFail("hwc_evs_disk", "creating disk "+name, res.res), nil
	}
	r := Changed("hwc_evs_disk: " + name + " created")
	if res.completed {
		id, _ := jobEntityString(res.job, "volume_id")
		if id != "" {
			r = r.WithExtra("id", id)
		}
	} else {
		r = Result{Msg: "hwc_evs_disk: " + name + " creation accepted, not confirmed within this port's poll window", Changed: true}
		r = r.WithExtra("job_status", "RUNNING")
	}
	return r, nil
}

// hcloudJobResult is the outcome of one CreateX/DeleteX call whose
// response carries a job_id this port then polls to completion (or its
// own bounded poll window) via hcloudPollJob — shared by
// hwc_evs_disk.go and hwc_ecs_instance.go, this batch's only two
// async, job-based hwc_* modules.
type hcloudJobResult struct {
	res       remoteexec.Result
	job       map[string]any
	completed bool
	failed    bool
}

// hcloudRunJSONJob runs one hcloud invocation expected to return a
// job_id, then polls it via hcloudPollJob. failed=true covers both "the
// initial request itself failed" and "the job completed with
// job_status=FAIL" — either way the caller Fails with hcloudFail(...,
// res) or the job's own fail_reason.
func hcloudRunJSONJob(ctx context.Context, conn remoteexec.Connection, service, operation string, params map[string]string, region map[string]string) (hcloudJobResult, error) {
	var initial map[string]any
	res, err := hcloudRunJSON(ctx, conn, service, operation, params, &initial)
	if err != nil {
		return hcloudJobResult{}, err
	}
	if res.RC != 0 {
		return hcloudJobResult{res: res, failed: true}, nil
	}
	jobID := fmt.Sprint(initial["job_id"])
	if jobID == "" || jobID == "<nil>" {
		// Nothing to poll — treat as already complete (some Huawei
		// operations, and every scripted fakeConn test in this
		// package, return synchronously with no job_id at all).
		return hcloudJobResult{res: res, job: initial, completed: true}, nil
	}
	status, completed, failed, err := hcloudPollJob(ctx, conn, service, jobID, region)
	if err != nil {
		return hcloudJobResult{}, err
	}
	if failed {
		msg := fmt.Sprint(status["fail_reason"])
		return hcloudJobResult{res: remoteexec.Result{RC: 1, Stderr: msg}, failed: true}, nil
	}
	return hcloudJobResult{res: res, job: status, completed: completed}, nil
}

// jobEntityString reads job["entities"][key] as a string, matching the
// shape Huawei's own ECS/EVS job-status responses use to report the
// id of the resource a job just created ({"entities": {"volume_id":
// "...", ...}}).
func jobEntityString(job map[string]any, key string) (string, bool) {
	entities, ok := job["entities"].(map[string]any)
	if !ok {
		return "", false
	}
	v, ok := entities[key]
	if !ok {
		return "", false
	}
	return fmt.Sprint(v), true
}
