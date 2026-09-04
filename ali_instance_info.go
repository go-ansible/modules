package modules

import (
	"fmt"
	"sort"
	"strings"

	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAliInstanceInfo implements Ansible's `ali_instance_info`
// (community.general) module: fetches Alibaba Cloud ECS instances via
// `aliyun ecs DescribeInstances` — see ali_common.go's own doc comment
// for why this port substitutes the `aliyun` CLI for real
// ali_instance_info's own footmark-SDK-based ECS API calls, and for the
// alicloud_access_key/alicloud_secret_key/alicloud_security_token
// wiring and alicloud_region requirement.
//
// Args: alicloud_region (required, aliases region/region_id);
// name_prefix — sent as `--InstanceName '<prefix>*'`, using
// DescribeInstances' own documented fuzzy/wildcard InstanceName match
// (Alibaba Cloud's own API reference documents InstanceName as
// supporting a trailing `*` wildcard); tags (dict, aliases
// instance_tags) — sent as indexed `--Tag.N.Key`/`--Tag.N.Value` pairs,
// the same convention ali_instance.go's own aliTagResourcesArgv uses;
// filters (dict) — each entry is sent straight through as one
// `--<Key> <value>` pair (a list value is JSON-array-encoded first,
// matching real ali_instance_info's own doc for `InstanceIds`); a
// filter key already containing an uppercase letter is assumed to
// already be a real DescribeInstances parameter name and passed
// through unchanged; an all-lowercase/underscore/dash key (e.g.
// `instance_ids`, `vpc-id`) is converted to PascalCase first (matching
// real ali_instance_info's own doc: "Filter keys can be same as
// request parameter name or be lower case and use underscore or dash
// to connect different words").
//
// Extra["ids"]: every returned instance's own InstanceId.
// Extra["instances"]: DescribeInstances' own JSON objects, unchanged
// PascalCase field names — see moduleAliInstance's own doc comment for
// the same documented shape deviation from real ali_instance_info's
// own footmark-derived snake_case Extra["instances"].
func moduleAliInstanceInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "ali_instance_info"
	region, err := requireString(args, "alicloud_region")
	if err != nil {
		return Result{}, err
	}
	if res, ok := aliyunRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}
	env := aliyunEnv(argString(args, "alicloud_access_key", ""), argString(args, "alicloud_secret_key", ""), argString(args, "alicloud_security_token", ""))

	var extra []string
	if prefix := argString(args, "name_prefix", ""); prefix != "" {
		extra = append(extra, "--InstanceName", prefix+"*")
	}
	if tags := aliTagsArg(args); len(tags) > 0 {
		keys := make([]string, 0, len(tags))
		for k := range tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			extra = append(extra, fmt.Sprintf("--Tag.%d.Key", i+1), k, fmt.Sprintf("--Tag.%d.Value", i+1), tags[k])
		}
	}
	if filters, ok := args["filters"].(map[string]any); ok {
		keys := make([]string, 0, len(filters))
		for k := range filters {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			extra = append(extra, "--"+aliFilterParamName(k), aliFilterValue(filters[k]))
		}
	}

	instances, res, err := aliyunDescribeInstances(ctx, conn, env, region, extra...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return aliyunFail(mod, "describing instances", res), nil
	}
	return Ok("").WithExtra("ids", instanceIDsOf(instances)).WithExtra("instances", instances), nil
}

// aliFilterParamName converts a filters dict key to a DescribeInstances
// parameter name — passed through unchanged if it already contains an
// uppercase letter (assumed to already be a real parameter name),
// otherwise converted from snake_case/kebab-case to PascalCase.
func aliFilterParamName(key string) string {
	for _, r := range key {
		if r >= 'A' && r <= 'Z' {
			return key
		}
	}
	parts := strings.FieldsFunc(key, func(r rune) bool { return r == '_' || r == '-' })
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}
	return b.String()
}

// aliFilterValue renders one filters dict value for aliyun-cli: a list
// is JSON-array-encoded (matching real ali_instance_info's own doc for
// `InstanceIds`); anything else is fmt.Sprint'd.
func aliFilterValue(v any) string {
	switch x := v.(type) {
	case []any:
		ss := make([]string, len(x))
		for i, e := range x {
			ss[i] = fmt.Sprint(e)
		}
		return jsonStringArray(ss)
	case []string:
		return jsonStringArray(x)
	default:
		return fmt.Sprint(v)
	}
}
