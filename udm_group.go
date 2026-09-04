package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleUdmGroup implements Ansible's `udm_group` (community.general)
// module: manages a POSIX group on a Univention Corporate Server (UCS).
// See udmBin's own doc comment (udm_common.go) for this port's `udm`
// CLI substitution.
//
// Args: name (string, required); description (string, optional);
// state (present|absent, default "present"); position (string, default
// "") — the group's own full LDAP container DN, taking precedence over
// ou/subpath when non-empty; ou (string, default "") — wrapped as
// "ou=<ou>,"; subpath (string, default "cn=groups") — wrapped as
// "<subpath>,". When position is empty the container is built as
// "<subpath><ou><base_dn>", matching real udm_group.py's own string
// concatenation exactly (an empty ou or subpath contributes nothing,
// rather than a stray comma).
func moduleUdmGroup(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("udm_group: state must be present or absent, got %q", state)
	}

	container, err := udmGroupContainer(ctx, conn, args)
	if err != nil {
		return Result{}, err
	}
	modulePath := "groups/group"
	scope := udmScope{Position: container}

	obj, err := udmFind(ctx, conn, modulePath, "name="+name, scope)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if obj == nil {
			return Ok(name + " already absent"), nil
		}
		if err := udmRemove(ctx, conn, modulePath, obj.DN); err != nil {
			return Result{}, err
		}
		return Changed(name+" removed").WithExtra("container", container), nil
	}

	desired := map[string][]string{"name": {name}}
	if _, ok := args["description"]; ok {
		desired["description"] = []string{argString(args, "description", "")}
	}

	if obj == nil {
		if err := udmCreate(ctx, conn, modulePath, scope, desired); err != nil {
			return Result{}, err
		}
		return Changed(name+" created").WithExtra("container", container), nil
	}
	changed, err := udmReconcile(ctx, conn, modulePath, obj, desired)
	if err != nil {
		return Result{}, err
	}
	if !changed {
		return Ok(name+" already up to date").WithExtra("container", container), nil
	}
	return Changed(name+" updated").WithExtra("container", container), nil
}

// udmGroupContainer computes the group's LDAP container, matching real
// udm_group.py's own `container = position or (subpath + ou +
// base_dn())` logic exactly (see moduleUdmGroup's own doc comment).
func udmGroupContainer(ctx context.Context, conn remoteexec.Connection, args map[string]any) (string, error) {
	if position := argString(args, "position", ""); position != "" {
		return position, nil
	}
	baseDN, err := udmBaseDN(ctx, conn)
	if err != nil {
		return "", err
	}
	container := baseDN
	if ou := argString(args, "ou", ""); ou != "" {
		container = "ou=" + ou + "," + container
	}
	if subpath := argString(args, "subpath", "cn=groups"); subpath != "" {
		container = subpath + "," + container
	}
	return container, nil
}
