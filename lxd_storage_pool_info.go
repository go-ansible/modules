package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// lxdStoragePool is the subset of the LXD API's own storage-pool
// object (GET /1.0/storage-pools?recursion=1) this port reads.
type lxdStoragePool struct {
	Name        string         `json:"name"`
	Driver      string         `json:"driver"`
	Description string         `json:"description"`
	Config      map[string]any `json:"config"`
	Locations   []string       `json:"locations"`
	Status      string         `json:"status"`
	UsedBy      []string       `json:"used_by"`
}

// moduleLxdStoragePoolInfo implements Ansible's `lxd_storage_pool_info`
// (community.general) module: a read-only listing of LXD storage
// pools, via `lxc query GET /1.0/storage-pools?recursion=1` — see
// lxdBin's own doc comment for why this port substitutes the `lxc` CLI
// for real lxd_storage_pool_info's pylxd REST client; `lxc query` was
// chosen (over `lxc storage list`) for the identical reason
// lxdGetProfile's own doc comment (lxd_profile.go) gives: it decodes
// the same JSON shape pylxd's own REST client would see, `recursion=1`
// giving the full per-pool object (config/description/driver/
// locations/status/used_by) in one round trip instead of `lxc storage
// list`'s own summarized table.
//
// Args: name — if set, only that pool (Extra["storage_pools"] has at
// most one entry; a pool that doesn't exist returns an empty list, not
// a failure, matching real lxd_storage_pool_info's own tolerant
// handling); project; type ([]string) — filters the result to pools
// whose driver is in this list, done client-side in Go since `lxc
// query`'s own GET has no server-side driver filter.
//
// Extra["storage_pools"]: a list of maps with
// config/description/driver/locations/name/status/used_by, matching
// real lxd_storage_pool_info's own documented return shape exactly
// (this port reads the same LXD API response real lxd_storage_pool_info
// itself decodes, just via a CLI hop instead of a direct HTTP request).
// Never Changed — this module only ever reads.
func moduleLxdStoragePoolInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path := "/1.0/storage-pools?recursion=1"
	if p := argString(args, "project", ""); p != "" {
		path += "&project=" + p
	}
	argv := []string{lxdBin, "query", "GET", path}
	res, err := lxdRun(ctx, conn, argv)
	if err != nil {
		return Result{}, fmt.Errorf("lxd_storage_pool_info: running lxc query: %w", err)
	}
	if res.RC != 0 {
		return Fail("lxd_storage_pool_info: " + strings.TrimSpace(res.Stderr)), nil
	}
	var pools []lxdStoragePool
	if err := json.Unmarshal([]byte(res.Stdout), &pools); err != nil {
		return Result{}, fmt.Errorf("lxd_storage_pool_info: parsing lxc query output: %w", err)
	}

	name := argString(args, "name", "")
	types := argStringList(args, "type")

	out := make([]any, 0, len(pools))
	for _, p := range pools {
		if name != "" && p.Name != name {
			continue
		}
		if len(types) > 0 && !stringSliceContains(types, p.Driver) {
			continue
		}
		out = append(out, map[string]any{
			"name":        p.Name,
			"driver":      p.Driver,
			"description": p.Description,
			"config":      p.Config,
			"locations":   p.Locations,
			"status":      p.Status,
			"used_by":     p.UsedBy,
		})
	}
	return Ok("").WithExtra("storage_pools", out), nil
}

func stringSliceContains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
