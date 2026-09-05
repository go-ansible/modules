package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

var xccRedfishCommandCategories = map[string][]string{
	"Manager": {"VirtualMediaInsert", "VirtualMediaEject"},
	"Raw":     {"GetResource", "GetCollectionResource", "PatchResource", "PostResource"},
}

// moduleXccRedfishCommand implements Ansible's `xcc_redfish_command`
// module for Lenovo ThinkSystem servers' XClarity Controller (XCC), via
// Lenovo's own official "XClarity Essentials OneCLI" (`OneCli`/
// `OneCli.exe`) — confirmed, via Lenovo's own OneCLI User Guide (read
// before writing this file), as a real tool that explicitly supports
// forcing Redfish communication with XCC (`--redfish`) and manages
// virtual media, accounts, and configuration.
//
// Real xcc_redfish_command.py implements two independent command
// families:
//
//   - category=Manager: VirtualMediaInsert/VirtualMediaEject — insert or
//     eject a virtual CD/DVD/USB image.
//   - category=Raw: GetResource/GetCollectionResource/PatchResource/
//     PostResource — an ARBITRARY Redfish GET/PATCH/POST against a
//     caller-given resource_uri/request_body, with no fixed operation
//     vocabulary at all (its own EXAMPLES fetch OEM DNS settings, patch
//     an arbitrary AssetTag property, and POST to an arbitrary vendor
//     action URI).
//
// # OneCLI mapping: Manager category maps cleanly, Raw category does NOT
//
// OneCLI's own documented `vm` command family (verified against its own
// User Guide's "vm command syntax" table before writing this file) is a
// genuine, well-matched substitution for VirtualMediaInsert/Eject:
//
//	OneCli vm list  --bmc <conn>
//	OneCli vm mount  --id <slot> --path <nfs://|https://... url> [--writeprotected] --bmc <conn>
//	OneCli vm umount --id <slot> --bmc <conn>
//
// (`--bmc` is documented as OPTIONAL — omitting it targets the LOCAL
// BMC, which is what this port always does; see redfish_common.go's own
// doc comment.) This port picks the virtual-media slot (`--id`) by
// running `vm list` first and choosing the first slot OneCLI's own
// output reports as not currently mounted (mirroring real
// virtual_media_insert's own "find an empty slot" search, without this
// port also trying to intersect it against the requested media_types the
// way real _find_empty_virt_media_slot does, since OneCLI's own `vm
// list` output was not independently verified against a live XCC in
// this sandbox closely enough to parse a MediaTypes-equivalent column
// with confidence — a documented, narrower-than-upstream slot selection).
//
// The Raw category has NO OneCLI equivalent this port could find:
// OneCLI's own `--redfish` flag only forces its EXISTING config/
// attribute-oriented commands (`config get`/`set`/`show`/`showvalues`/
// `batch`) to use Redfish as their transport — none of those commands
// accept an arbitrary resource_uri and JSON request_body the way real
// GetResource/PatchResource/PostResource do; there is no `OneCli rawget`
// /`rawpatch`/`rawpost` the way ilorest genuinely has (see ilo_common.go's
// own doc comment on that contrast). Per this batch's own instruction to
// "fail loud for that specific operation/category rather than faking
// it" when no real equivalent exists, this port's category=Raw ALWAYS
// returns Result{Failed:true} naming this exact gap — it does not
// attempt a partial or best-effort translation.
//
// Args: category (required, "Manager" or "Raw"); command (required
// list); virtual_media (dict: image_url, media_types, write_protected,
// inserted — the last two are accepted for shape compatibility but
// unused: OneCLI's own `vm mount` has no "treat as inserted"
// distinction, always mounting write-enabled unless --writeprotected is
// given); resource_id, resource_uri, request_body accepted for shape
// compatibility (resource_uri/request_body are ONLY meaningful for the
// unsupported Raw category, so they have no effect here either).
// baseuri/username/password/auth_token/timeout accepted for shape
// compatibility, NO EFFECT (see redfish_common.go's own doc comment).
func moduleXccRedfishCommand(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	category, err := requireString(args, "category")
	if err != nil {
		return Result{}, err
	}
	commands := argStringList(args, "command")
	if len(commands) == 0 {
		return Result{}, errArg("xcc_redfish_command: missing required argument: command")
	}
	if res, ok := redfishCheckCategory("xcc_redfish_command", category, xccRedfishCommandCategories); !ok {
		return res, nil
	}
	if res, ok := redfishCheckCommands("xcc_redfish_command", category, commands, xccRedfishCommandCategories); !ok {
		return res, nil
	}

	if category == "Raw" {
		return Fail("xcc_redfish_command: category 'Raw' (GetResource/GetCollectionResource/PatchResource/" +
			"PostResource — an arbitrary Redfish GET/PATCH/POST against a caller-given resource_uri) has NO " +
			"Lenovo OneCLI equivalent: OneCLI's own --redfish flag only changes the TRANSPORT of its existing " +
			"config-attribute commands, it does not expose a generic raw-request passthrough the way HPE's " +
			"ilorest does (rawget/rawpatch/rawpost) — see xcc_redfish_command.go's own doc comment. This is a " +
			"documented, deliberate gap, not an attempted-but-wrong translation."), nil
	}

	if res, ok := xccRequireBinary(ctx, conn, "xcc_redfish_command"); !ok {
		return res, nil
	}

	vm, _ := args["virtual_media"].(map[string]any)
	changed := false
	for _, command := range commands {
		switch command {
		case "VirtualMediaInsert":
			res, err := xccVirtualMediaInsert(ctx, conn, vm)
			if err != nil {
				return Result{}, err
			}
			if res.Failed {
				return res, nil
			}
			changed = changed || res.Changed
		case "VirtualMediaEject":
			res, err := xccVirtualMediaEject(ctx, conn, vm)
			if err != nil {
				return Result{}, err
			}
			if res.Failed {
				return res, nil
			}
			changed = changed || res.Changed
		}
	}
	if changed {
		return Changed("Action was successful"), nil
	}
	return Ok("Action was successful"), nil
}

func xccRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	return redfishRequireBinary(ctx, conn, moduleName, "OneCli",
		"this port shells out to Lenovo's own local XClarity Essentials OneCLI rather than speaking Redfish "+
			"HTTPS directly — see xcc_redfish_command.go's own doc comment and redfish_common.go's own doc "+
			"comment on this batch's local/in-band CLI architecture")
}

// xccVMSlot is one row of `OneCli vm list`'s own output, as best-effort
// parsed by xccParseVMList below.
type xccVMSlot struct {
	ID      string
	Mounted bool
}

func xccVirtualMediaInsert(ctx context.Context, conn remoteexec.Connection, vm map[string]any) (Result, error) {
	imageURL, _ := vm["image_url"].(string)
	if imageURL == "" {
		return Fail("xcc_redfish_command: VirtualMediaInsert: image_url is required"), nil
	}
	slots, err := xccVMList(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	for _, s := range slots {
		if s.Mounted {
			continue
		}
		writeProtected, _ := vm["write_protected"].(bool)
		cmd := "OneCli vm mount --id " + shellQuote(s.ID) + " --path " + shellQuote(imageURL)
		if writeProtected {
			cmd += " --writeprotected"
		}
		res, err := runStatus(ctx, conn, cmd)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("xcc_redfish_command: VirtualMediaInsert: " + xccErrMsg(res)), nil
		}
		return Changed("VirtualMedia inserted"), nil
	}
	return Fail("xcc_redfish_command: VirtualMediaInsert: no available (unmounted) virtual media slot found"), nil
}

func xccVirtualMediaEject(ctx context.Context, conn remoteexec.Connection, vm map[string]any) (Result, error) {
	slots, err := xccVMList(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	ejectedAny := false
	for _, s := range slots {
		if !s.Mounted {
			continue
		}
		res, err := runStatus(ctx, conn, "OneCli vm umount --id "+shellQuote(s.ID))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("xcc_redfish_command: VirtualMediaEject: " + xccErrMsg(res)), nil
		}
		ejectedAny = true
	}
	if !ejectedAny {
		return Ok("No VirtualMedia image inserted"), nil
	}
	return Changed("VirtualMedia ejected"), nil
}

func xccVMList(ctx context.Context, conn remoteexec.Connection) ([]xccVMSlot, error) {
	res, err := runStatus(ctx, conn, "OneCli vm list")
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, nil
	}
	return xccParseVMList(res.Stdout), nil
}

// xccParseVMList is a best-effort, honestly-bounded parse of `OneCli vm
// list`'s own output — this port has no live XCC/OneCLI binary in its
// own sandbox to capture real output against (see xcc_redfish_command.go's
// own doc comment). It looks for a slot-ID-shaped first token per line
// (matching the User Guide's own example IDs: RDOC1, EXT1, Remote1,
// CD) and treats any line whose remaining text mentions "mount" without
// "unmount"/"not mounted" as currently mounted.
func xccParseVMList(output string) []xccVMSlot {
	var out []xccVMSlot
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		id := fields[0]
		if id == "ID" || strings.HasPrefix(id, "-") {
			continue
		}
		rest := strings.ToLower(strings.Join(fields[1:], " "))
		mounted := strings.Contains(rest, "mounted") && !strings.Contains(rest, "not mounted") && !strings.Contains(rest, "unmounted")
		out = append(out, xccVMSlot{ID: id, Mounted: mounted})
	}
	return out
}

func xccErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}
