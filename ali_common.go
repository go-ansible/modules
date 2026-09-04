package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what the two ali_*.go modules in this batch
// (ali_instance, ali_instance_info) share: shelling out to `aliyun`
// (Alibaba Cloud's own official CLI, aliyun-cli,
// github.com/aliyun/aliyun-cli) instead of talking to Alibaba Cloud's
// ECS API through the `footmark` Python SDK the way every real
// ali_instance*.py module in this batch does — the same "shell out to
// the platform's own official CLI instead of an API client" precedent
// this port already uses for Consul, Redis, Terraform, Icinga2, Kopia,
// GitHub (`gh`), GitLab (`glab`), Keycloak (`kcadm.sh`), and Scaleway
// (`scw`) — a deliberate, user-approved architectural decision for this
// batch, not a gap.
//
// # `aliyun` invocation shape — verified, not guessed
//
// aliyun-cli's own calling convention (confirmed against Alibaba
// Cloud's own published CLI documentation and ECS CLI reference, e.g.
// alibabacloud.com/help/en/ecs/developer-reference/cli-reference) is
// RPC-style: `aliyun <product> <Action> --Param value ...` — a product
// namespace ("ecs"), a PascalCase API action name ("CreateInstance",
// "DescribeInstances", "StartInstance", "StopInstance",
// "RebootInstance", "DeleteInstance", "JoinSecurityGroup",
// "LeaveSecurityGroup", "TagResources", "UntagResources" — every one of
// these is a real, individually-documented ECS API action, not
// invented from the module name), then `--Param value` pairs matching
// that action's own documented request parameters exactly (PascalCase,
// e.g. --RegionId, --ImageId, --InstanceType — not the snake_case this
// port's own module arguments use). `--output cols=... rows=Instance`
// selects a tabular projection for a human; this port instead relies on
// aliyun-cli's own default JSON response body (confirmed from the same
// CLI reference's own CreateInstance/DescribeInstances examples, both
// of which show a raw JSON object/array as the default, undecorated
// response) and decodes that directly, with no `--output`/`-o` flag
// needed at all (unlike scw's own `-o json`/gh's own `--json` — aliyun
// CLI's default output already IS the API's own JSON response body).
//
// # Auth precondition
//
// `aliyun` must already be configured on the TARGET host before any
// ali_* module in this port runs: either a prior `aliyun configure`
// (which writes ~/.aliyun/config.json, aliyun-cli's own traditional
// credentials file) has already run there, or
// ALIBABA_CLOUD_ACCESS_KEY_ID/ALIBABA_CLOUD_ACCESS_KEY_SECRET/
// ALIBABA_CLOUD_SECURITY_TOKEN are already exported in that session's
// own environment (aliyun-cli's own current documented env var names,
// confirmed from Alibaba Cloud's own CLI environment-variables
// reference — NOT the older ALICLOUD_ACCESS_KEY/ALICLOUD_SECRET_KEY
// names real ali_instance's own footmark-based auth arguments/NOTES
// section documents, which are footmark's own env vars, not aliyun-cli's)
// — the same shape of precondition github_common.go's own doc comment
// sets for a pre-existing `gh auth login`/GH_TOKEN, and scaleway_common.go's
// for a pre-existing `scw init`. This port does not attempt to drive
// `aliyun configure` (an interactive credential-entry ceremony) itself.
//
// Every real ali_instance*.py module's own alicloud_access_key/
// alicloud_secret_key/alicloud_security_token arguments (aliased
// access_key_id/access_key, secret_access_key/secret_key,
// security_token) ARE wired through when given — as the
// ALIBABA_CLOUD_ACCESS_KEY_ID/ALIBABA_CLOUD_ACCESS_KEY_SECRET/
// ALIBABA_CLOUD_SECURITY_TOKEN environment variables for that single
// invocation only, never as a `--AccessKeyId`/`--AccessKeySecret`
// command-line flag — this project's own hard "no secrets in argv"
// rule (see redis.go's own REDISCLI_AUTH precedent). When none are
// given, `aliyun` falls back to its own already-configured profile/env
// as-is. alicloud_assume_role/alicloud_assume_role_arn/
// alicloud_assume_role_session_name/alicloud_assume_role_session_expiration/
// profile/shared_credentials_file/ecs_role_name are all accepted (for
// argument-shape compatibility with real playbooks) but have NO EFFECT
// on this port's behavior — a deliberate, honestly-documented gap,
// matching ipa_common.go's own stance exactly: `aliyun` has its own
// `--profile`/RAM-role-on-ECS-metadata support, configured the same
// out-of-band way as the base credentials above, not a per-invocation
// override this port drives.
//
// # Region
//
// alicloud_region (aliased region/region_id, required by every real
// ali_instance*.py module) is passed straight through as `--RegionId`
// on every aliyun invocation this port makes — ECS's own API requires
// it on every action, matching real ali_instance's own required-ness.

// aliyunRequireBinary fails cleanly (Result{Failed:true}, not a Go
// error) if the real `aliyun` CLI is not on the target's PATH.
func aliyunRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v aliyun"); err != nil {
		return Fail(fmt.Sprintf("%s: the aliyun binary (Alibaba Cloud's own official CLI, aliyun-cli) is "+
			"required on the target and was not found in PATH — this port shells out to it rather than "+
			"speaking Alibaba Cloud's ECS API via the footmark SDK directly; see ali_common.go's own doc "+
			"comment, including the precondition that `aliyun configure` must already have been run (or "+
			"ALIBABA_CLOUD_ACCESS_KEY_ID/ALIBABA_CLOUD_ACCESS_KEY_SECRET already set) on the target", moduleName)), false
	}
	return Result{}, true
}

// aliyunEnv resolves the ALIBABA_CLOUD_ACCESS_KEY_ID/
// ALIBABA_CLOUD_ACCESS_KEY_SECRET/ALIBABA_CLOUD_SECURITY_TOKEN
// environment-variable prefix for one aliyun invocation from this
// port's own alicloud_access_key/alicloud_secret_key/
// alicloud_security_token arguments — empty when none are given (see
// ali_common.go's own doc comment on why these are the ONLY
// credential-shaped arguments this port wires through, and why never
// as argv).
func aliyunEnv(accessKey, secretKey, securityToken string) string {
	var parts []string
	if accessKey != "" {
		parts = append(parts, "ALIBABA_CLOUD_ACCESS_KEY_ID="+shellQuote(accessKey))
	}
	if secretKey != "" {
		parts = append(parts, "ALIBABA_CLOUD_ACCESS_KEY_SECRET="+shellQuote(secretKey))
	}
	if securityToken != "" {
		parts = append(parts, "ALIBABA_CLOUD_SECURITY_TOKEN="+shellQuote(securityToken))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ") + " "
}

// aliyunCmd renders one `aliyun <argv...>` invocation, shell-quoting
// each argv entry, prefixed by env (see aliyunEnv).
func aliyunCmd(env string, argv ...string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return env + "aliyun " + strings.Join(quoted, " ")
}

// aliyunRun runs one `aliyun` invocation on conn and returns its raw
// result — RC not treated as an error, callers decide what a nonzero
// exit means.
func aliyunRun(ctx context.Context, conn remoteexec.Connection, env string, argv ...string) (remoteexec.Result, error) {
	return runStatus(ctx, conn, aliyunCmd(env, argv...))
}

// aliyunRunJSON runs argv (via aliyunRun) and decodes its stdout as
// JSON into out on success (aliyun-cli's own default response body IS
// the ECS API's own JSON — see ali_common.go's own doc comment, no
// --output flag needed).
func aliyunRunJSON(ctx context.Context, conn remoteexec.Connection, env string, out any, argv ...string) (remoteexec.Result, error) {
	res, err := aliyunRun(ctx, conn, env, argv...)
	if err != nil {
		return res, err
	}
	if res.RC != 0 || out == nil || strings.TrimSpace(res.Stdout) == "" {
		return res, nil
	}
	if jerr := json.Unmarshal([]byte(res.Stdout), out); jerr != nil {
		return res, fmt.Errorf("decoding aliyun %s output: %w", strings.Join(argv, " "), jerr)
	}
	return res, nil
}

// aliyunErrMsg builds a Fail() message body from a nonzero aliyun
// result, preferring stderr but falling back to stdout.
func aliyunErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}

// aliyunFail builds a Fail() Result for one failed aliyun invocation.
func aliyunFail(moduleName, action string, res remoteexec.Result) Result {
	return Fail(fmt.Sprintf("%s: %s: %s", moduleName, action, aliyunErrMsg(res)))
}

// aliInstance is the subset of ECS's own DescribeInstances response
// object this port's two ali_*.go modules need — decoded straight
// from aliyun-cli's own JSON, PascalCase field names unchanged
// (matching aliyun-cli's own output shape, NOT real ali_instance's own
// footmark-derived snake_case Extra["instances"] shape — see each
// module's own doc comment for that documented deviation).
type aliInstance struct {
	InstanceId    string `json:"InstanceId"`
	InstanceName  string `json:"InstanceName"`
	Status        string `json:"Status"`
	RegionId      string `json:"RegionId"`
	ImageId       string `json:"ImageId"`
	InstanceType  string `json:"InstanceType"`
	HostName      string `json:"HostName"`
	Description   string `json:"Description"`
	VpcAttributes struct {
		VSwitchId string `json:"VSwitchId"`
		VpcId     string `json:"VpcId"`
	} `json:"VpcAttributes"`
	SecurityGroupIds struct {
		SecurityGroupId []string `json:"SecurityGroupId"`
	} `json:"SecurityGroupIds"`
	Tags struct {
		Tag []struct {
			TagKey   string `json:"TagKey"`
			TagValue string `json:"TagValue"`
		} `json:"Tag"`
	} `json:"Tags"`
}

// aliyunDescribeInstances runs `aliyun ecs DescribeInstances --RegionId
// region <extra...>` and decodes the "Instances.Instance" array every
// real DescribeInstances JSON response nests its results under
// (confirmed from Alibaba Cloud's own ECS API reference: the response
// envelope is {"Instances":{"Instance":[...]}, "PageNumber":...,
// "TotalCount":...,...}).
func aliyunDescribeInstances(ctx context.Context, conn remoteexec.Connection, env, region string, extra ...string) ([]aliInstance, remoteexec.Result, error) {
	argv := append([]string{"ecs", "DescribeInstances", "--RegionId", region}, extra...)
	var raw struct {
		Instances struct {
			Instance []aliInstance `json:"Instance"`
		} `json:"Instances"`
	}
	res, err := aliyunRunJSON(ctx, conn, env, &raw, argv...)
	return raw.Instances.Instance, res, err
}
