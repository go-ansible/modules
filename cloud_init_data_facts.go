package modules

import (
	"context"
	"encoding/json"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleCloudInitDataFacts implements Ansible's `cloud_init_data_facts`
// module: reads cloud-init's own local `status.json` and `result.json`
// files on the target and returns their parsed content as facts, read
// from real cloud_init_data_facts.py's own source (plugins/modules/
// cloud_init_data_facts.py) — a pure local file read, no cloud API call
// of any kind (matching this batch's own task description).
//
// ⚠ The files read are `/var/lib/cloud/data/status.json` and
// `/var/lib/cloud/data/result.json` — verified directly from real
// cloud_init_data_facts.py's own `CLOUD_INIT_PATH = "/var/lib/cloud/
// data"` constant. This is NOT `/run/cloud-init/instance-data.json`
// (cloud-init's own separate, differently-shaped instance-metadata
// cache) — per this batch's own hard rule to read the reference source
// before implementing rather than guessing from a module name, this
// port reads the exact two files the real module reads and no others.
//
// Args: filter (choices status|result, optional) — when given, only
// that one file is read; when omitted, both are read (an absent file is
// not an error either way — matching real cloud_init_data_facts' own
// `if os.path.exists(json_file):` guard, which just leaves that key as
// an empty dict when the file doesn't exist, e.g. before cloud-init has
// run or on a host where it isn't installed at all).
//
// Facts: returned under BOTH `ansible_facts.cloud_init_data_facts` and,
// matching real cloud_init_data_facts' own `dict(changed=changed,
// ansible_facts=facts, **facts)` (which spreads the SAME dict at both
// the ansible_facts level and the result's own top level), this port's
// Result.Extra["cloud_init_data_facts"] carries the identical dict —
// both surfaces present, exactly like real cloud_init_data_facts.
//
// Never Changed — this module only ever reads.
func moduleCloudInitDataFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	filter := argString(args, "filter", "")
	if filter != "" && filter != "status" && filter != "result" {
		return Result{}, errArg("cloud_init_data_facts: filter must be status or result, got %q", filter)
	}

	data := map[string]any{}
	for _, name := range []string{"result", "status"} {
		if filter != "" && filter != name {
			continue
		}
		path := cloudInitDataPath + "/" + name + ".json"
		exists, err := pathExists(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			data[name] = map[string]any{}
			continue
		}
		contents, err := run(ctx, conn, "cat "+shellQuote(path))
		if err != nil {
			return Result{}, err
		}
		contents = strings.TrimSpace(contents)
		if contents == "" {
			data[name] = map[string]any{}
			continue
		}
		var parsed any
		if err := json.Unmarshal([]byte(contents), &parsed); err != nil {
			return Fail("cloud_init_data_facts: parsing " + path + ": " + err.Error()), nil
		}
		data[name] = parsed
	}

	res := Ok("gathered cloud-init data facts").WithExtra("cloud_init_data_facts", data)
	res.Facts = map[string]any{"cloud_init_data_facts": data}
	return res, nil
}

// cloudInitDataPath is real cloud_init_data_facts' own CLOUD_INIT_PATH.
const cloudInitDataPath = "/var/lib/cloud/data"
