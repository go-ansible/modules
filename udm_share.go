package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// udmShareBoolField describes one of udm_share's many boolean options:
// key is both the module argument's own primary (camelCase) name and
// the UDM attribute it is sent under unchanged (real udm_share.py's own
// `for k in obj.keys(): obj[k] = module.params[k]` loop uses the exact
// same key for both, since the argument_spec's primary key names ARE
// the UDM object's own property names — the snake_case forms are only
// Ansible-style aliases on top).
type udmShareBoolField struct {
	key, alias string
	def        bool
}

type udmShareStrField struct {
	key, alias, def string
}

type udmShareListField struct {
	key, alias string
}

var udmShareBoolFields = []udmShareBoolField{
	{"root_squash", "", true},
	{"subtree_checking", "", true},
	{"writeable", "", true},
	{"sambaBlockingLocks", "samba_blocking_locks", true},
	{"sambaBrowseable", "samba_browsable", true},
	{"sambaDosFilemode", "samba_dos_filemode", false},
	{"sambaFakeOplocks", "samba_fake_oplocks", false},
	{"sambaForceCreateMode", "samba_force_create_mode", false},
	{"sambaForceDirectoryMode", "samba_force_directory_mode", false},
	{"sambaForceDirectorySecurityMode", "samba_force_directory_security_mode", false},
	{"sambaForceSecurityMode", "samba_force_security_mode", false},
	{"sambaHideUnreadable", "samba_hide_unreadable", false},
	{"sambaInheritAcls", "samba_inherit_acls", true},
	{"sambaInheritOwner", "samba_inherit_owner", false},
	{"sambaInheritPermissions", "samba_inherit_permissions", false},
	{"sambaLevel2Oplocks", "samba_level_2_oplocks", true},
	{"sambaLocking", "samba_locking", true},
	{"sambaMSDFSRoot", "samba_msdfs_root", false},
	{"sambaNtAclSupport", "samba_nt_acl_support", true},
	{"sambaOplocks", "samba_oplocks", true},
	{"sambaPublic", "samba_public", false},
	{"sambaWriteable", "samba_writeable", true},
}

var udmShareStrFields = []udmShareStrField{
	{"owner", "", "0"},
	{"group", "", "0"},
	{"directorymode", "", "00755"},
	{"sync", "", "sync"},
	{"sambaBlockSize", "samba_block_size", ""},
	{"sambaCreateMode", "samba_create_mode", "0744"},
	{"sambaCscPolicy", "samba_csc_policy", "manual"},
	{"sambaDirectoryMode", "samba_directory_mode", "0755"},
	{"sambaDirectorySecurityMode", "samba_directory_security_mode", "0777"},
	{"sambaForceGroup", "samba_force_group", ""},
	{"sambaForceUser", "samba_force_user", ""},
	{"sambaHideFiles", "samba_hide_files", ""},
	{"sambaInvalidUsers", "samba_invalid_users", ""},
	{"sambaSecurityMode", "samba_security_mode", "0777"},
	{"sambaStrictLocking", "samba_strict_locking", "Auto"},
	{"sambaVFSObjects", "samba_vfs_objects", ""},
	{"sambaValidUsers", "samba_valid_users", ""},
	{"sambaWriteList", "samba_write_list", ""},
}

var udmShareListFields = []udmShareListField{
	{"sambaHostsAllow", "samba_hosts_allow"},
	{"sambaHostsDeny", "samba_hosts_deny"},
	{"nfs_hosts", ""},
	{"nfsCustomSettings", "nfs_custom_settings"},
}

// moduleUdmShare implements Ansible's `udm_share` (community.general)
// module: manages a Samba/NFS network share on a Univention Corporate
// Server (UCS). See udmBin's own doc comment (udm_common.go) for this
// port's `udm` CLI substitution.
//
// Args: name (string, required); ou (string, required) — the share's
// LDAP organisational unit, its container is always
// "cn=shares,ou=<ou>,<base_dn>", matching real udm_share.py's own
// `container = f"cn=shares,ou={ou},{base_dn()}"` exactly (unlike
// udm_group/udm_user this module has no position/subpath override);
// state (present|absent, default "present"); path (path, required when
// state=present); host (string, required when state=present) — also
// used to compute the read-only "printablename" UDM attribute as
// "<name> (<host>)", matching real udm_share.py's own
// `module.params["printablename"] = f"{name} ({host})"`; sambaName
// (string, required when state=present, alias samba_name); owner/group
// (default "0"); directorymode (default "00755"); root_squash/
// subtree_checking/writeable (bool, default true); sync (default
// "sync"); every samba* option documented by real udm_share.py (Samba
// share behavior: browseable, oplocks, ACL/security/create/directory
// modes, hosts allow/deny, force user/group, valid/invalid/write-list
// users, MSDFS root, and so on — see udmShareBoolFields/
// udmShareStrFields/udmShareListFields above for the exact set and
// their defaults, each accepting both its camelCase primary name and
// its snake_case alias, matching the real module's own argument_spec);
// nfs_hosts ([]string, default []); nfsCustomSettings ([]string,
// default [], alias nfs_custom_settings).
//
// sambaCustomSettings (list of {key, value} dicts, default []) is a
// deviation this port documents rather than hides: real udm_share.py
// hands this compound multi-valued property straight to
// univention.admin's own Python API, which knows its native
// serialization; this port has no way to exercise a live `udm` CLI to
// confirm the exact `--set`/`--append` text format that property
// expects on the command line, so it sends one `--set
// sambaCustomSettings=<key> <value>` (space-joined) per entry — a
// best-effort, UNVERIFIED mapping. If a UCS installation rejects that
// syntax, set the property directly via `udm shares/share modify
// --append sambaCustomSettings=...` in a follow-up `command` task
// instead of trusting this port's guess.
//
// State semantics: create when absent (via `--position
// cn=shares,ou=<ou>,<base_dn>`); reconcile (single `udm shares/share
// modify --dn <dn>` carrying every changed attribute) when present and
// any managed attribute differs — see udmReconcile's own doc comment;
// remove when state=absent and the share exists.
func moduleUdmShare(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	ou, err := requireString(args, "ou")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("udm_share: state must be present or absent, got %q", state)
	}

	baseDN, err := udmBaseDN(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	container := "cn=shares,ou=" + ou + "," + baseDN
	modulePath := "shares/share"
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
		return Changed(name + " removed").WithExtra("container", container), nil
	}

	path, err := requireString(args, "path")
	if err != nil {
		return Result{}, err
	}
	host, err := requireString(args, "host")
	if err != nil {
		return Result{}, err
	}
	sambaName := argStringAliased(args, "sambaName", "samba_name", "")
	if sambaName == "" {
		return Result{}, errArg("udm_share: sambaName is required when state=present")
	}

	desired := map[string][]string{
		"name":          {name},
		"path":          {path},
		"host":          {host},
		"sambaName":     {sambaName},
		"printablename": {fmt.Sprintf("%s (%s)", name, host)},
	}
	for _, f := range udmShareBoolFields {
		desired[f.key] = []string{udmBoolStr(argBoolAliased(args, f.key, f.alias, f.def))}
	}
	for _, f := range udmShareStrFields {
		if v := argStringAliased(args, f.key, f.alias, f.def); v != "" {
			desired[f.key] = []string{v}
		}
	}
	for _, f := range udmShareListFields {
		if v := argStringListAliased(args, f.key, f.alias); len(v) > 0 {
			desired[f.key] = v
		}
	}
	if v := udmShareCustomSettings(args); len(v) > 0 {
		desired["sambaCustomSettings"] = v
	}

	if obj == nil {
		if err := udmCreate(ctx, conn, modulePath, scope, desired); err != nil {
			return Result{}, err
		}
		return Changed(name + " created").WithExtra("container", container), nil
	}
	changed, err := udmReconcile(ctx, conn, modulePath, obj, desired)
	if err != nil {
		return Result{}, err
	}
	if !changed {
		return Ok(name + " already up to date").WithExtra("container", container), nil
	}
	return Changed(name + " updated").WithExtra("container", container), nil
}

// udmShareCustomSettings converts the sambaCustomSettings module
// argument (a list of {key, value} dicts) into the "<key> <value>"
// space-joined string form this port sends — see moduleUdmShare's own
// doc comment for why this mapping is best-effort and unverified.
func udmShareCustomSettings(args map[string]any) []string {
	v, ok := args["sambaCustomSettings"]
	if !ok {
		v, ok = args["samba_custom_settings"]
	}
	if !ok {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		k := fmt.Sprint(m["key"])
		val := fmt.Sprint(m["value"])
		out = append(out, strings.TrimSpace(k+" "+val))
	}
	return out
}
