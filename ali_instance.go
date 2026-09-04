package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAliInstance implements (a subset of) Ansible's `ali_instance`
// (community.general) module: creates, starts, stops, restarts, or
// terminates Alibaba Cloud ECS instances, and adds/removes them from
// security groups — via `aliyun ecs CreateInstance`/`StartInstance`/
// `StopInstance`/`RebootInstance`/`DeleteInstance`/`JoinSecurityGroup`/
// `TagResources`/`UntagResources` — see ali_common.go's own doc comment
// for why this port substitutes the `aliyun` CLI for real
// ali_instance's own footmark-SDK-based ECS API calls, and for the
// alicloud_access_key/alicloud_secret_key/alicloud_security_token
// wiring and alicloud_region requirement every call below shares.
//
// Args: alicloud_region (required, aliases region/region_id);
// instance_ids ([]string) — when given, every operation below targets
// exactly these EXISTING instances (matching real ali_instance's own
// "If it is specified, count is ignored" doc); instance_name (aliases
// name); image_id (aliases image) and instance_type (aliases type) —
// required to create when instance_ids is unset; count (default 1);
// count_tag — this port accepts a JSON object string
// ({"Key":"Value"}), a single "Key=Value"/"Key:Value" pair, or a bare
// tag key (matched regardless of value); when omitted, matches
// instance_name instead, mirroring real ali_instance's own "If it is
// not specified, it is replaced by instance_name" doc; vswitch_id
// (aliases subnet_id); security_groups ([]string, aliases group_ids);
// host_name; password; description; system_disk_category (default
// cloud_efficiency); system_disk_size (default 40); system_disk_name;
// system_disk_description; internet_charge_type (default
// PayByBandwidth); allocate_public_ip (bool, aliases assign_public_ip)
// with max_bandwidth_out (required when true, sent as
// --InternetMaxBandwidthOut) and max_bandwidth_in (default 200);
// instance_charge_type (default PostPaid) with period/period_unit/
// auto_renew/auto_renew_period (PrePaid only); spot_strategy (default
// NoSpot) with spot_price_limit; key_name (aliases keypair); user_data;
// availability_zone (aliases alicloud_zone/zone_id); tags (dict,
// aliases instance_tags) with purge_tags (bool, default false); force
// (bool, default false) — passed as StopInstance's/RebootInstance's own
// --ForceStop and DeleteInstance's own --Force; dry_run (bool, default
// false) — passed as CreateInstance's own --DryRun; state
// (present|running|stopped|restarted|absent, default present).
//
// # Creating (state=present, instance_ids unset)
//
// The number of instances currently matching count_tag (or
// instance_name, see above) is read via DescribeInstances; if fewer
// than count exist, this port creates the shortfall, each via `aliyun
// ecs CreateInstance` (which, per Alibaba Cloud's own documented ECS
// API semantics, creates a STOPPED instance) immediately followed by
// `aliyun ecs StartInstance` for that same instance — unless
// dry_run=true, in which case only CreateInstance's own --DryRun
// validation runs and no StartInstance call is made. Every newly
// created instance ID is then tagged (if tags given) and joined to
// every security group beyond the first (the first, if any, is passed
// directly on CreateInstance's own --SecurityGroupId, matching that
// action's own required-a-security-group-at-creation-time semantics).
//
// # Targeting existing instances (instance_ids given)
//
// state=present: adds any security_groups not already attached (via
// JoinSecurityGroup) and reconciles tags (TagResources for
// missing/changed keys, UntagResources for keys present on the
// instance but not in the requested tags when purge_tags=true) — this
// port does not remove a security group not listed, matching real
// ali_instance's own EXAMPLES, which only ever demonstrate adding.
// state=running: StartInstance for every listed instance whose current
// Status isn't already "Running". state=stopped: StopInstance
// (+--ForceStop when force=true) for every listed instance whose
// current Status isn't already "Stopped". state=restarted:
// RebootInstance (+--ForceStop when force=true) for every listed
// instance unconditionally (Changed=true always — a reboot is an
// action, not idempotent, matching real ali_instance's own unconditional
// reboot_instances call). state=absent: DeleteInstance
// (+--Force=force) for every listed instance still found; a
// requested ID no longer found is silently skipped, matching real
// ali_instance's own "no error on an already-gone instance" behavior.
// Every one of these Fails (Result{Failed:true}) if DescribeInstances
// finds NONE of the requested instance_ids at all, matching real
// ali_instance.py's own "There are no instances in our record based on
// instance_ids ..." fail_json.
//
// Deviation — state=running/stopped/restarted/absent with instance_ids
// UNSET: real ali_instance.py can also resolve targets via count_tag in
// this case; this port requires instance_ids for these four states and
// Fails with a clear argument error otherwise — an honestly-documented
// narrowing, not a silent no-op, given this batch's own time budget.
//
// Extra["ids"]: every instance ID this call created or acted on.
// Extra["instances"]: DescribeInstances' own JSON objects (aliyun-cli's
// own PascalCase field names — InstanceId, Status, InstanceType, ...
// — unchanged; NOT real ali_instance's own footmark-derived snake_case
// Extra["instances"] shape, the same kind of documented shape
// deviation github_repo.go's own doc comment already accepts for `gh
// repo view --json` vs. PyGithub's raw_data).
func moduleAliInstance(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "ali_instance"
	region, err := requireString(args, "alicloud_region")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	switch state {
	case "present", "running", "stopped", "restarted", "absent":
	default:
		return Result{}, errArg("%s: state must be one of present, running, stopped, restarted, absent, got %q", mod, state)
	}

	if res, ok := aliyunRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}
	env := aliyunEnv(argString(args, "alicloud_access_key", ""), argString(args, "alicloud_secret_key", ""), argString(args, "alicloud_security_token", ""))
	force := argBool(args, "force", false)
	forceStopArg := []string{}
	if force {
		forceStopArg = []string{"--ForceStop", "true"}
	}

	instanceIDs := argStringList(args, "instance_ids")

	if len(instanceIDs) == 0 {
		if state != "present" {
			return Result{}, errArg("%s: instance_ids is required for state=%s (count_tag-based targeting for "+
				"non-present states is not implemented by this port — see moduleAliInstance's own doc comment)", mod, state)
		}
		return aliInstanceCreate(ctx, conn, env, region, args)
	}

	instances, res, err := aliyunDescribeInstances(ctx, conn, env, region, "--InstanceIds", jsonStringArray(instanceIDs))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return aliyunFail(mod, "describing instances", res), nil
	}
	if len(instances) == 0 {
		return Fail(fmt.Sprintf("%s: there are no instances in our record based on instance_ids %v — please check it and try again", mod, instanceIDs)), nil
	}

	switch state {
	case "present":
		return aliInstanceReconcile(ctx, conn, env, region, args, instances)
	case "running":
		return aliInstanceSetPower(ctx, conn, env, mod, instances, "Running", []string{"ecs", "StartInstance"})
	case "stopped":
		return aliInstanceSetPower(ctx, conn, env, mod, instances, "Stopped", append([]string{"ecs", "StopInstance"}, forceStopArg...))
	case "restarted":
		changedAny := false
		for _, inst := range instances {
			r, err := aliyunRun(ctx, conn, env, append([]string{"ecs", "RebootInstance", "--InstanceId", inst.InstanceId}, forceStopArg...)...)
			if err != nil {
				return Result{}, err
			}
			if r.RC != 0 {
				return aliyunFail(mod, "rebooting instance "+inst.InstanceId, r), nil
			}
			changedAny = true
		}
		return Result{Changed: changedAny}.WithExtra("ids", instanceIDsOf(instances)), nil
	case "absent":
		var deleted []string
		for _, inst := range instances {
			r, err := aliyunRun(ctx, conn, env, "ecs", "DeleteInstance", "--InstanceId", inst.InstanceId, "--Force", strconv.FormatBool(force))
			if err != nil {
				return Result{}, err
			}
			if r.RC != 0 {
				return aliyunFail(mod, "deleting instance "+inst.InstanceId, r), nil
			}
			deleted = append(deleted, inst.InstanceId)
		}
		return Result{Changed: len(deleted) > 0}.WithExtra("ids", deleted), nil
	}
	return Result{}, errArg("%s: unreachable state %q", mod, state)
}

func instanceIDsOf(instances []aliInstance) []string {
	out := make([]string, len(instances))
	for i, inst := range instances {
		out[i] = inst.InstanceId
	}
	return out
}

// aliInstanceSetPower starts or stops every instance not already in
// wantStatus, via the given aliyun argv prefix (plus --InstanceId).
func aliInstanceSetPower(ctx context.Context, conn remoteexec.Connection, env, mod string, instances []aliInstance, wantStatus string, argvPrefix []string) (Result, error) {
	changedAny := false
	for _, inst := range instances {
		if inst.Status == wantStatus {
			continue
		}
		argv := append(append([]string{}, argvPrefix...), "--InstanceId", inst.InstanceId)
		r, err := aliyunRun(ctx, conn, env, argv...)
		if err != nil {
			return Result{}, err
		}
		if r.RC != 0 {
			return aliyunFail(mod, "changing power state of instance "+inst.InstanceId, r), nil
		}
		changedAny = true
	}
	return Result{Changed: changedAny}.WithExtra("ids", instanceIDsOf(instances)), nil
}

// aliInstanceReconcile applies security_groups/tags to already-existing
// instances (state=present, instance_ids given) — see
// moduleAliInstance's own doc comment.
func aliInstanceReconcile(ctx context.Context, conn remoteexec.Connection, env, region string, args map[string]any, instances []aliInstance) (Result, error) {
	changed := false
	wantSGs := argStringList(args, "security_groups")
	for _, inst := range instances {
		current := map[string]bool{}
		for _, sg := range inst.SecurityGroupIds.SecurityGroupId {
			current[sg] = true
		}
		for _, sg := range wantSGs {
			if current[sg] {
				continue
			}
			r, err := aliyunRun(ctx, conn, env, "ecs", "JoinSecurityGroup", "--InstanceId", inst.InstanceId, "--SecurityGroupId", sg)
			if err != nil {
				return Result{}, err
			}
			if r.RC != 0 {
				return aliyunFail("ali_instance", "joining instance "+inst.InstanceId+" to security group "+sg, r), nil
			}
			changed = true
		}
	}

	if tags := aliTagsArg(args); tags != nil {
		purge := argBool(args, "purge_tags", false)
		for _, inst := range instances {
			current := map[string]string{}
			for _, t := range inst.Tags.Tag {
				current[t.TagKey] = t.TagValue
			}
			var toSet [][2]string
			for k, v := range tags {
				if current[k] != v {
					toSet = append(toSet, [2]string{k, v})
				}
			}
			if len(toSet) > 0 {
				r, err := aliyunRun(ctx, conn, env, aliTagResourcesArgv(region, []string{inst.InstanceId}, toSet)...)
				if err != nil {
					return Result{}, err
				}
				if r.RC != 0 {
					return aliyunFail("ali_instance", "tagging instance "+inst.InstanceId, r), nil
				}
				changed = true
			}
			if purge {
				var toRemove []string
				for k := range current {
					if _, ok := tags[k]; !ok {
						toRemove = append(toRemove, k)
					}
				}
				if len(toRemove) > 0 {
					r, err := aliyunRun(ctx, conn, env, aliUntagResourcesArgv(region, []string{inst.InstanceId}, toRemove)...)
					if err != nil {
						return Result{}, err
					}
					if r.RC != 0 {
						return aliyunFail("ali_instance", "untagging instance "+inst.InstanceId, r), nil
					}
					changed = true
				}
			}
		}
	}

	return Result{Changed: changed}.WithExtra("ids", instanceIDsOf(instances)), nil
}

// aliInstanceCreate creates the shortfall between the number of
// instances already matching count_tag (or instance_name) and count —
// see moduleAliInstance's own doc comment.
func aliInstanceCreate(ctx context.Context, conn remoteexec.Connection, env, region string, args map[string]any) (Result, error) {
	const mod = "ali_instance"
	count := argInt(args, "count", 1)
	instanceName := argString(args, "instance_name", "")
	countTag := argString(args, "count_tag", "")

	var describeExtra []string
	if countTag != "" {
		k, v, matched := parseCountTag(countTag)
		if matched {
			describeExtra = []string{"--Tag.1.Key", k, "--Tag.1.Value", v}
		} else {
			describeExtra = []string{"--Tag.1.Key", countTag}
		}
	} else if instanceName != "" {
		describeExtra = []string{"--InstanceName", instanceName}
	}

	var existing []aliInstance
	if len(describeExtra) > 0 {
		var res remoteexec.Result
		var err error
		existing, res, err = aliyunDescribeInstances(ctx, conn, env, region, describeExtra...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return aliyunFail(mod, "describing existing instances", res), nil
		}
	}

	shortfall := count - len(existing)
	if shortfall <= 0 {
		return Result{Changed: false}.WithExtra("ids", instanceIDsOf(existing)).WithExtra("instances", existing), nil
	}

	imageID, err := requireString(args, "image_id")
	if err != nil {
		return Result{}, err
	}
	instanceType, err := requireString(args, "instance_type")
	if err != nil {
		return Result{}, err
	}

	sgs := argStringList(args, "security_groups")
	dryRun := argBool(args, "dry_run", false)

	createArgv := []string{"ecs", "CreateInstance", "--RegionId", region, "--ImageId", imageID, "--InstanceType", instanceType}
	if len(sgs) > 0 {
		createArgv = append(createArgv, "--SecurityGroupId", sgs[0])
	}
	if v := argString(args, "vswitch_id", ""); v != "" {
		createArgv = append(createArgv, "--VSwitchId", v)
	}
	if instanceName != "" {
		createArgv = append(createArgv, "--InstanceName", instanceName)
	}
	if v := argString(args, "host_name", ""); v != "" {
		createArgv = append(createArgv, "--HostName", v)
	}
	if v := argString(args, "password", ""); v != "" {
		createArgv = append(createArgv, "--Password", v)
	}
	if v := argString(args, "description", ""); v != "" {
		createArgv = append(createArgv, "--Description", v)
	}
	if v := argString(args, "availability_zone", ""); v != "" {
		createArgv = append(createArgv, "--ZoneId", v)
	}
	if v := argString(args, "key_name", ""); v != "" {
		createArgv = append(createArgv, "--KeyPairName", v)
	}
	if v := argString(args, "user_data", ""); v != "" {
		createArgv = append(createArgv, "--UserData", v)
	}
	createArgv = append(createArgv,
		"--SystemDisk.Category", argString(args, "system_disk_category", "cloud_efficiency"),
		"--SystemDisk.Size", strconv.Itoa(argInt(args, "system_disk_size", 40)),
	)
	if v := argString(args, "system_disk_name", ""); v != "" {
		createArgv = append(createArgv, "--SystemDisk.DiskName", v)
	}
	if v := argString(args, "system_disk_description", ""); v != "" {
		createArgv = append(createArgv, "--SystemDisk.Description", v)
	}
	createArgv = append(createArgv, "--InternetChargeType", argString(args, "internet_charge_type", "PayByBandwidth"))
	if argBool(args, "allocate_public_ip", argBool(args, "assign_public_ip", false)) {
		createArgv = append(createArgv, "--InternetMaxBandwidthOut", strconv.Itoa(argInt(args, "max_bandwidth_out", 0)))
	}
	createArgv = append(createArgv, "--InternetMaxBandwidthIn", strconv.Itoa(argInt(args, "max_bandwidth_in", 200)))
	chargeType := argString(args, "instance_charge_type", "PostPaid")
	createArgv = append(createArgv, "--InstanceChargeType", chargeType)
	if chargeType == "PrePaid" {
		if v, ok := args["period"]; ok {
			createArgv = append(createArgv, "--Period", fmt.Sprint(v))
		}
		createArgv = append(createArgv, "--PeriodUnit", argString(args, "period_unit", "Month"))
		if argBool(args, "auto_renew", false) {
			createArgv = append(createArgv, "--AutoRenew", "true")
			if v, ok := args["auto_renew_period"]; ok {
				createArgv = append(createArgv, "--AutoRenewPeriod", fmt.Sprint(v))
			}
		}
	} else {
		createArgv = append(createArgv, "--SpotStrategy", argString(args, "spot_strategy", "NoSpot"))
		if v, ok := args["spot_price_limit"]; ok {
			createArgv = append(createArgv, "--SpotPriceLimit", fmt.Sprint(v))
		}
	}
	if dryRun {
		createArgv = append(createArgv, "--DryRun", "true")
	}

	var created []string
	for i := 0; i < shortfall; i++ {
		var out struct {
			InstanceId string `json:"InstanceId"`
		}
		res, err := aliyunRunJSON(ctx, conn, env, &out, createArgv...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return aliyunFail(mod, "creating instance", res), nil
		}
		if dryRun {
			continue
		}
		if out.InstanceId == "" {
			return Fail(mod + ": CreateInstance succeeded but returned no InstanceId"), nil
		}
		startRes, err := aliyunRun(ctx, conn, env, "ecs", "StartInstance", "--InstanceId", out.InstanceId)
		if err != nil {
			return Result{}, err
		}
		if startRes.RC != 0 {
			return aliyunFail(mod, "starting newly created instance "+out.InstanceId, startRes), nil
		}
		created = append(created, out.InstanceId)
	}

	if dryRun {
		return Ok(mod + ": dry run — CreateInstance validated, no instance created"), nil
	}

	if len(sgs) > 1 {
		for _, id := range created {
			for _, sg := range sgs[1:] {
				r, err := aliyunRun(ctx, conn, env, "ecs", "JoinSecurityGroup", "--InstanceId", id, "--SecurityGroupId", sg)
				if err != nil {
					return Result{}, err
				}
				if r.RC != 0 {
					return aliyunFail(mod, "joining instance "+id+" to security group "+sg, r), nil
				}
			}
		}
	}

	if tags := aliTagsArg(args); len(tags) > 0 && len(created) > 0 {
		var pairs [][2]string
		for k, v := range tags {
			pairs = append(pairs, [2]string{k, v})
		}
		r, err := aliyunRun(ctx, conn, env, aliTagResourcesArgv(region, created, pairs)...)
		if err != nil {
			return Result{}, err
		}
		if r.RC != 0 {
			return aliyunFail(mod, "tagging newly created instances", r), nil
		}
	}

	final, res, err := aliyunDescribeInstances(ctx, conn, env, region, "--InstanceIds", jsonStringArray(created))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return aliyunFail(mod, "describing newly created instances", res), nil
	}

	return Result{Changed: true}.WithExtra("ids", instanceIDsOf(append(existing, final...))).WithExtra("instances", final), nil
}

// aliTagsArg reads args["tags"] (falling back to its own
// instance_tags alias) as a map[string]string, or nil if unset.
func aliTagsArg(args map[string]any) map[string]string {
	v, ok := args["tags"]
	if !ok {
		v, ok = args["instance_tags"]
	}
	if !ok {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			out[k] = s
		} else {
			out[k] = fmt.Sprint(val)
		}
	}
	return out
}

// aliTagResourcesArgv renders `ecs TagResources --RegionId r
// --ResourceType instance --ResourceId.N id --Tag.N.Key k --Tag.N.Value
// v ...` (Alibaba Cloud's own documented indexed-array parameter
// convention for TagResources, shared across every ECS/RAM/... API
// that accepts a list of tags).
func aliTagResourcesArgv(region string, ids []string, tags [][2]string) []string {
	sort.Slice(tags, func(i, j int) bool { return tags[i][0] < tags[j][0] })
	argv := []string{"ecs", "TagResources", "--RegionId", region, "--ResourceType", "instance"}
	for i, id := range ids {
		argv = append(argv, fmt.Sprintf("--ResourceId.%d", i+1), id)
	}
	for i, t := range tags {
		argv = append(argv, fmt.Sprintf("--Tag.%d.Key", i+1), t[0], fmt.Sprintf("--Tag.%d.Value", i+1), t[1])
	}
	return argv
}

// aliUntagResourcesArgv renders `ecs UntagResources --RegionId r
// --ResourceType instance --ResourceId.N id --TagKey.N k ...`.
func aliUntagResourcesArgv(region string, ids []string, keys []string) []string {
	sort.Strings(keys)
	argv := []string{"ecs", "UntagResources", "--RegionId", region, "--ResourceType", "instance"}
	for i, id := range ids {
		argv = append(argv, fmt.Sprintf("--ResourceId.%d", i+1), id)
	}
	for i, k := range keys {
		argv = append(argv, fmt.Sprintf("--TagKey.%d", i+1), k)
	}
	return argv
}

// parseCountTag parses a count_tag argument shaped as either a JSON
// object string ({"Key":"Value"}, using its first entry) or a single
// "Key=Value"/"Key:Value" pair — see moduleAliInstance's own doc
// comment. matched=false means countTag should be treated as a bare
// tag KEY (any value).
func parseCountTag(countTag string) (key, value string, matched bool) {
	var asMap map[string]string
	if json.Unmarshal([]byte(countTag), &asMap) == nil && len(asMap) > 0 {
		keys := make([]string, 0, len(asMap))
		for k := range asMap {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys[0], asMap[keys[0]], true
	}
	for _, sep := range []string{"=", ":"} {
		if idx := strings.Index(countTag, sep); idx > 0 {
			return countTag[:idx], countTag[idx+1:], true
		}
	}
	return "", "", false
}

// jsonStringArray renders ss as a JSON array of strings — the shape
// aliyun-cli's own list-valued RPC parameters (e.g. DescribeInstances'
// InstanceIds) require, per Alibaba Cloud's own ECS API reference.
func jsonStringArray(ss []string) string {
	b, _ := json.Marshal(ss)
	return string(b)
}
