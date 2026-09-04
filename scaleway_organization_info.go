package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayOrganizationInfo implements Ansible's
// `scaleway_organization_info` (community.general) module: gathers
// information about the Scaleway organizations available to the
// authenticated account.
//
// Deviation — no real `scw` CLI equivalent exists for this module's
// real behavior, and this is disclosed honestly rather than faked.
// Real scaleway_organization_info calls GET /organizations against
// account.scaleway.com and returns a LIST of full organization
// resources — each with address_city_name/address_country_code/
// currency/customer_class/locale/name/support_id/support_level/
// support_pin/users/vat_number/warnings, per its own RETURN VALUES
// sample. `scw`'s own published CLI reference has no command that
// returns any of this: `scw account project` (verified,
// scaleway-cli's own docs/commands/account.md) is PROJECT management
// only (create/delete/get/list/update), and `scw iam organization`
// (verified, docs/commands/iam.md) only has enable-saml/get-saml/
// security-settings — no generic "get my organization's identity/
// billing details" command exists in `scw` at all, because that
// account/billing-profile API predates the CLI's own project-centric,
// IAM-centric model and was never wrapped by it.
//
// This port's best-effort substitute: `scw config get
// default_organization_id`, which reads the organization ID already
// configured in `scw`'s own local profile (see scaleway_common.go's
// own doc comment on the auth precondition) — verified as a real `scw
// config get <key>` invocation against scaleway-cli's own
// docs/commands/config.md. This returns AT MOST one organization ID
// (the locally configured default, not a live list fetched from the
// account), and NONE of the real module's own address/name/support/
// VAT/users fields — a caller relying on those fields will find them
// absent here, which is the honest outcome given no `scw` command
// surfaces them, not a best-effort fabrication of plausible-looking
// values.
//
// Extra["organizations"]: []map[string]any, holding a single
// {"id": "<default_organization_id>"} entry when `scw config get
// default_organization_id` succeeds and is non-empty, else an empty
// list — matching real scaleway_organization_info's own list return
// TYPE, not its content.
func moduleScalewayOrganizationInfo(ctx context.Context, conn remoteexec.Connection, _ map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_organization_info"); !ok {
		return res, nil
	}
	res, err := runStatus(ctx, conn, "scw config get default_organization_id")
	if err != nil {
		return Result{}, err
	}
	orgs := []map[string]any{}
	if res.RC == 0 {
		// `scw config get` on an unset key prints an empty line, not a
		// nonzero exit, per `scw`'s own documented behavior for a
		// missing config key.
		if id := strings.TrimSpace(res.Stdout); id != "" {
			orgs = append(orgs, map[string]any{"id": id})
		}
	}
	return Ok("").WithExtra("organizations", orgs), nil
}
