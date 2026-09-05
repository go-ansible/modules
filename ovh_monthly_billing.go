package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleOvhMonthlyBilling implements Ansible's `ovh_monthly_billing`
// module via OVHcloud's own official `ovhcloud-cli` — see
// ovh_common.go's own doc comment for the CLI substitution rationale
// and credential wiring. Unlike its two sibling modules in this batch
// (ovh_ip_failover.go/ovh_ip_loadbalancing_backend.go, both blocked by a
// verified missing-verb gap), this module's own core operation has a
// genuine, dedicated ovhcloud-cli verb with no gap at all:
// `ovhcloud cloud instance activate-monthly-billing <instance_id>
// --cloud-project <project_id>` — verified directly against
// ovhcloud-cli's own doc/ovhcloud_cloud_instance_activate-monthly-
// billing.md.
//
// Args: project_id (string, required); instance_id (string, required);
// endpoint/application_key/application_secret/consumer_key (all
// optional, matching real ovh_monthly_billing.py — unlike its two
// sibling modules, none of these four is required here).
//
// Idempotency matches real ovh_monthly_billing.py exactly:
// `ovhcloud cloud instance get <instance_id> --cloud-project
// <project_id>` is checked first, and if its own `monthlyBilling`
// field is already non-null with a `status` of "ok" or
// "activationPending", this module reports Ok/unchanged without
// calling activate-monthly-billing at all — real OVH billing, once
// enabled, can never be disabled again (this module's own real
// counterpart's short_description says so explicitly: "be aware OVH
// does not allow to disable it"), so re-running this module is always
// safe.
func moduleOvhMonthlyBilling(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := ovhRequireBinary(ctx, conn, "ovh_monthly_billing"); !ok {
		return res, nil
	}
	projectID, err := requireString(args, "project_id")
	if err != nil {
		return Result{}, errArg("ovh_monthly_billing: %v", err)
	}
	instanceID, err := requireString(args, "instance_id")
	if err != nil {
		return Result{}, errArg("ovh_monthly_billing: %v", err)
	}
	env := ovhEnv(args)

	var instance map[string]any
	res, err := ovhRunJSON(ctx, conn, env, &instance, "cloud", "instance", "get", instanceID, "--cloud-project", projectID)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("ovh_monthly_billing: instance %s does not exist in project %s: %s", instanceID, projectID, ovhErrMsg(res))), nil
	}

	if billing, ok := instance["monthlyBilling"].(map[string]any); ok && billing != nil {
		if status := fmt.Sprint(billing["status"]); status == "ok" || status == "activationPending" {
			return Ok("monthly billing already enabled or pending").WithExtra("ovh_billing_status", billing), nil
		}
	}

	var activated map[string]any
	ares, err := ovhRunJSON(ctx, conn, env, &activated, "cloud", "instance", "activate-monthly-billing", instanceID, "--cloud-project", projectID)
	if err != nil {
		return Result{}, err
	}
	if ares.RC != 0 {
		return Fail(fmt.Sprintf("ovh_monthly_billing: failed to activate monthly billing: %s", ovhErrMsg(ares))), nil
	}

	status := activated
	if billing, ok := activated["monthlyBilling"].(map[string]any); ok {
		status = billing
	}
	return Changed("monthly billing activated").WithExtra("ovh_billing_status", status), nil
}
