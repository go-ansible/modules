package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayDatabaseBackup implements Ansible's
// `scaleway_database_backup` (community.general) module: creates,
// updates, deletes, exports (generates a download link for), or
// restores (to a new database) a Scaleway Database backup, via `scw rdb
// backup create/get/update/delete/export/restore` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI for real scaleway_database_backup's own direct REST API
// calls, and for the auth/wait deviations shared by every scaleway_*
// module in this batch.
//
// Args: state (present|absent|exported|restored, default present); id
// — required for absent/exported/restored (matches real
// scaleway_database_backup's own required_if exactly, verified against
// scaleway_database_backup.py's own main()); name, database_name,
// instance_id — all three required for present (same required_if);
// database_name/instance_id also required together for restored;
// expires_at (ISO 8601 string, optional); region (required,
// fr-par|nl-ams|pl-waw).
//
// present: when id is unset, OR given but not found, this port always
// CREATES a new backup (`scw rdb backup create instance-id=...
// name=... database-name=... [expires-at=...] region=...`,
// Changed=true unconditionally) — matching real present_strategy's own
// behavior exactly: it only ever attempts to look a backup up by id
// when id was actually given, and only patches an EXISTING one found
// that way; every example in scaleway_database_backup's own
// EXAMPLES calls state=present with no id at all, so "always creates"
// is the common case, not a corner case this port invented. When id
// WAS given and found, this port compares the backup's own current
// name/expires_at against the requested ones (an unset name/expires_at
// argument always matches, per real present_strategy's own `backup["name"]
// == name or name is None` check) and issues `scw rdb backup update`
// only for an actual difference.
//
// absent: `scw rdb backup get <id>` first; Changed=false if not found
// (a 404 is not a failure — see scwNotFound), else `scw rdb backup
// delete <id> region=...` (Changed=true).
//
// exported: `scw rdb backup get <id>` first; Fail() if not found
// (matching real exported_strategy's own fail_json when backup is
// None); Changed=false if the backup already has a download_url set;
// else `scw rdb backup export <id> region=...` (Changed=true).
//
// restored: `scw rdb backup get <id>` first; Fail() if not found
// (matching real restored_strategy's own fail_json); always `scw rdb
// backup restore <id> instance-id=... database-name=... region=...`
// (Changed=true unconditionally — real restored_strategy has no
// idempotency check of its own either).
//
// Extra["metadata"]: `scw`'s own JSON object for the backup — matching
// real scaleway_database_backup's own identically-named return key,
// returned for present/exported/restored (never for absent, matching
// real RETURN VALUES' own "returned: when state=present, state=exported,
// or state=restored").
func moduleScalewayDatabaseBackup(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_database_backup"); !ok {
		return res, nil
	}
	region, err := scwRegionArg(args, "scaleway_database_backup")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent", "exported", "restored":
	default:
		return Result{}, errArg("scaleway_database_backup: state must be one of present, absent, exported, restored, got %q", state)
	}
	id := argString(args, "id", "")
	if state != "present" && id == "" {
		return Result{}, errArg("scaleway_database_backup: id is required when state is %s", state)
	}

	var current map[string]any
	var exists bool
	if id != "" {
		res, err := scwRunJSON(ctx, conn, "rdb", "backup", "get", id, "region="+region)
		if err != nil {
			return Result{}, err
		}
		if res.RC == 0 {
			exists = true
			if derr := scwDecode(res.Stdout, &current); derr != nil {
				return Result{}, derr
			}
		} else if !scwNotFound(res) {
			return Fail("scaleway_database_backup: failed to get backup " + id + ": " + scwErrMsg(res)), nil
		}
	}

	switch state {
	case "absent":
		if !exists {
			return Ok(""), nil
		}
		res, err := scwRun(ctx, conn, "rdb", "backup", "delete", id, "region="+region)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("scaleway_database_backup: failed to delete backup " + id + ": " + scwErrMsg(res)), nil
		}
		return Changed(""), nil

	case "exported":
		if !exists {
			return Fail("scaleway_database_backup: backup " + id + " not found"), nil
		}
		if dl, ok := current["download_url"]; ok && dl != nil {
			return Ok("").WithExtra("metadata", current), nil
		}
		res, err := scwRunJSON(ctx, conn, "rdb", "backup", "export", id, "region="+region)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("scaleway_database_backup: failed to export backup " + id + ": " + scwErrMsg(res)), nil
		}
		if derr := scwDecode(res.Stdout, &current); derr != nil {
			return Result{}, derr
		}
		return Changed("").WithExtra("metadata", current), nil

	case "restored":
		if !exists {
			return Fail("scaleway_database_backup: backup " + id + " not found"), nil
		}
		databaseName, err := requireString(args, "database_name")
		if err != nil {
			return Result{}, err
		}
		instanceID, err := requireString(args, "instance_id")
		if err != nil {
			return Result{}, err
		}
		res, err := scwRunJSON(ctx, conn, "rdb", "backup", "restore", id, "instance-id="+instanceID,
			"database-name="+databaseName, "region="+region)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("scaleway_database_backup: failed to restore backup " + id + ": " + scwErrMsg(res)), nil
		}
		if derr := scwDecode(res.Stdout, &current); derr != nil {
			return Result{}, derr
		}
		return Changed("").WithExtra("metadata", current), nil
	}

	// state == "present"
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	databaseName, err := requireString(args, "database_name")
	if err != nil {
		return Result{}, err
	}
	instanceID, err := requireString(args, "instance_id")
	if err != nil {
		return Result{}, err
	}
	expiresAt := argString(args, "expires_at", "")

	if exists {
		curName, _ := current["name"].(string)
		curExpires := ""
		if v, ok := current["expires_at"]; ok && v != nil {
			curExpires = fmt.Sprint(v)
		}
		nameMatches := name == "" || curName == name
		expiresMatches := expiresAt == "" || curExpires == expiresAt
		if nameMatches && expiresMatches {
			return Ok("").WithExtra("metadata", current), nil
		}
		argv := []string{"rdb", "backup", "update", id, "region=" + region}
		if name != "" {
			argv = append(argv, "name="+name)
		}
		if expiresAt != "" {
			argv = append(argv, "expires-at="+expiresAt)
		}
		res, err := scwRunJSON(ctx, conn, argv...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("scaleway_database_backup: failed to update backup " + id + ": " + scwErrMsg(res)), nil
		}
		if derr := scwDecode(res.Stdout, &current); derr != nil {
			return Result{}, derr
		}
		return Changed("").WithExtra("metadata", current), nil
	}

	argv := []string{"rdb", "backup", "create", "instance-id=" + instanceID, "name=" + name,
		"database-name=" + databaseName, "region=" + region}
	if expiresAt != "" {
		argv = append(argv, "expires-at="+expiresAt)
	}
	res, err := scwRunJSON(ctx, conn, argv...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("scaleway_database_backup: failed to create backup " + name + ": " + scwErrMsg(res)), nil
	}
	if derr := scwDecode(res.Stdout, &current); derr != nil {
		return Result{}, derr
	}
	return Changed("").WithExtra("metadata", current), nil
}
