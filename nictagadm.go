package modules

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleNictagadm implements Ansible's `nictagadm` (community.general)
// module: creates or deletes SmartOS network interface tags via
// `nictagadm(8)`.
//
// Args: name (string, required); state ("present"|"absent", default
// "present"); mac (string) — the MAC address to attach the tag to;
// required (and validated as a MAC address) unless etherstub is set,
// matching real nictagadm's own is_valid_mac() check, which real
// nictagadm's own module only runs when NOT creating an etherstub;
// etherstub (bool, default false) — attach the tag to a created
// etherstub instead of a MAC; mutually exclusive with both mac and
// mtu; mtu (int, optional) — mutually exclusive with etherstub; force
// (bool, default false) — `-f` on delete, ignoring existing VMs using
// the tag.
//
// Idempotency: presence is read via `nictagadm exists <name>` (exit 0
// meaning present), matching real nictagadm's own nictag_exists().
// Creation runs `nictagadm -v add [-l] [-p mtu=N] [-p mac=MAC] name`;
// deletion runs `nictagadm -v delete [-f] name` — both matching real
// nictagadm's own add_nictag()/delete_nictag() flag order exactly.
//
// Result.Extra always carries name/mac/etherstub/mtu/force/state,
// matching real nictagadm's own RETURN VALUES (all documented
// "returned: always").
func moduleNictagadm(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	mac := argString(args, "mac", "")
	etherstub := argBool(args, "etherstub", false)
	_, hasMTU := args["mtu"]
	mtu := argInt(args, "mtu", 0)
	force := argBool(args, "force", false)
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("nictagadm: state must be present or absent, got %q", state)
	}
	if etherstub && mac != "" {
		return Result{}, errArg("nictagadm: etherstub and mac are mutually exclusive")
	}
	if etherstub && hasMTU {
		return Result{}, errArg("nictagadm: etherstub and mtu are mutually exclusive")
	}

	extras := func(r Result) Result {
		r = r.WithExtra("name", name).WithExtra("mac", mac).WithExtra("etherstub", etherstub)
		r = r.WithExtra("mtu", mtu).WithExtra("force", force).WithExtra("state", state)
		return r
	}

	if !etherstub && !nictagadmValidMAC(mac) {
		return extras(Fail("Invalid MAC Address Value")), nil
	}

	exists, err := nictagadmExists(ctx, conn, name)
	if err != nil {
		return Result{}, err
	}

	switch state {
	case "absent":
		if !exists {
			return extras(Ok("")), nil
		}
		tokens := []string{"nictagadm", "-v", "delete"}
		if force {
			tokens = append(tokens, "-f")
		}
		tokens = append(tokens, name)
		res, err := runStatus(ctx, conn, quoteAll(tokens))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return extras(Fail(strings.TrimSpace(res.Stderr))).WithExtra("rc", res.RC), nil
		}
		return extras(Changed("")).WithExtra("stdout", res.Stdout), nil

	default: // present
		if exists {
			return extras(Ok("")), nil
		}
		tokens := []string{"nictagadm", "-v", "add"}
		if etherstub {
			tokens = append(tokens, "-l")
		}
		if hasMTU {
			tokens = append(tokens, "-p", fmt.Sprintf("mtu=%d", mtu))
		}
		if mac != "" {
			tokens = append(tokens, "-p", "mac="+mac)
		}
		tokens = append(tokens, name)
		res, err := runStatus(ctx, conn, quoteAll(tokens))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return extras(Fail(strings.TrimSpace(res.Stderr))).WithExtra("rc", res.RC), nil
		}
		return extras(Changed("")).WithExtra("stdout", res.Stdout), nil
	}
}

var nictagadmMacRE = regexp.MustCompile(`^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)

func nictagadmValidMAC(mac string) bool {
	if mac == "" {
		return false
	}
	return nictagadmMacRE.MatchString(strings.ToLower(mac))
}

func nictagadmExists(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	res, err := runStatus(ctx, conn, "nictagadm exists "+shellQuote(name))
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}
