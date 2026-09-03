package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// lxdStorageVolume is the subset of the LXD API's own storage-volume
// object (GET /1.0/storage-pools/<pool>/volumes?recursion=1) this port
// reads.
type lxdStorageVolume struct {
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	ContentType string         `json:"content_type"`
	Description string         `json:"description"`
	Config      map[string]any `json:"config"`
	Location    string         `json:"location"`
	UsedBy      []string       `json:"used_by"`
}

// moduleLxdStorageVolumeInfo implements Ansible's
// `lxd_storage_volume_info` (community.general) module: a read-only
// listing of the volumes in one LXD storage pool, via `lxc query GET
// /1.0/storage-pools/<pool>/volumes?recursion=1` — see
// lxd_storage_pool_info.go's own moduleLxdStoragePoolInfo doc comment
// for why this family reads through `lxc query` rather than `lxc
// storage volume list`'s own summarized table.
//
// Args: pool (required); name — if set, only that volume
// (Extra["storage_volumes"] has at most one entry; a volume that
// doesn't exist returns an empty list, not a failure); project; type —
// filters to that volume type (container|virtual-machine|image|
// custom), done client-side since `lxc query`'s own GET has no
// server-side type filter.
//
// Extra["storage_volumes"]: a list of maps with
// config/content_type/description/location/name/type/used_by,
// matching real lxd_storage_volume_info's own documented return shape.
// Never Changed — this module only ever reads.
func moduleLxdStorageVolumeInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	pool, err := requireString(args, "pool")
	if err != nil {
		return Result{}, err
	}
	path := "/1.0/storage-pools/" + pool + "/volumes?recursion=1"
	if p := argString(args, "project", ""); p != "" {
		path += "&project=" + p
	}
	argv := []string{lxdBin, "query", "GET", path}
	res, err := lxdRun(ctx, conn, argv)
	if err != nil {
		return Result{}, fmt.Errorf("lxd_storage_volume_info: running lxc query: %w", err)
	}
	if res.RC != 0 {
		return Fail("lxd_storage_volume_info: " + strings.TrimSpace(res.Stderr)), nil
	}
	var volumes []lxdStorageVolume
	if err := json.Unmarshal([]byte(res.Stdout), &volumes); err != nil {
		return Result{}, fmt.Errorf("lxd_storage_volume_info: parsing lxc query output: %w", err)
	}

	name := argString(args, "name", "")
	wantType := argString(args, "type", "")

	out := make([]any, 0, len(volumes))
	for _, v := range volumes {
		if name != "" && v.Name != name {
			continue
		}
		if wantType != "" && v.Type != wantType {
			continue
		}
		out = append(out, map[string]any{
			"name":         v.Name,
			"type":         v.Type,
			"content_type": v.ContentType,
			"description":  v.Description,
			"config":       v.Config,
			"location":     v.Location,
			"used_by":      v.UsedBy,
		})
	}
	return Ok("").WithExtra("storage_volumes", out), nil
}
