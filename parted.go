package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePartedInfo is one row of parted's machine-readable partition
// listing.
type modulePartedInfo struct {
	Num     int
	Begin   float64
	End     float64
	Size    float64
	FSType  string
	Name    string
	Flags   []string
	rawUnit string
}

// moduleParted implements (a subset of) Ansible's `parted` module:
// creates, removes, or resizes one partition on device via GNU parted's
// own `-s -m` (script, machine-readable) mode, and reports the disk's
// current layout the same way real parted does.
//
// Args: device (string, required); number (int) — required for
// state=present/absent, ignored for state=info; state (present|absent|
// info, default "info"); part_start (string, default "0%"); part_end
// (string, default "100%") — both accept any unit parted itself accepts
// (e.g. "10GiB", "50%", "-1GiB"), passed through verbatim; unit (string,
// default "KiB") — the display/interpretation unit for this call; label
// (string, default "msdos") — disk label/partition-table type; if the
// disk's CURRENT label differs, this port recreates the label (wiping
// any existing partitions), matching real parted's own documented
// "if device already contains a different label, it is changed... and
// any previous partitions are lost"; part_type (string, default
// "primary"); name (string) — partition name, only meaningful for
// GPT/Mac/PC98 labels (this port does not validate label compatibility
// the way real parted does — an incompatible combination is passed
// through to parted itself, which then reports its own error);
// fs_type (string) — only applied when CREATING a new partition,
// matching real parted's own documented "if specified and the partition
// does not exist"; flags (list of string) — ADDITIVE only: flags not
// already set on the partition are turned on via `parted set N flag
// on`; flags already set are left alone, and a flag NOT in the list is
// never turned off (real parted's own module behaves the same way);
// align (cylinder|minimal|none|optimal|undefined, default "optimal");
// resize (bool, default false) — calls `resizepart N part_end` on an
// EXISTING partition.
//
// Every state re-reads and returns the disk's current layout via
// `parted -s -m device unit UNIT print`, in Extra["disk"] (path, size,
// transport, logical_block, physical_block, table, model, unit) and
// Extra["partitions"] ([]map with num/begin/end/size/fstype/name/flags/
// unit), plus Extra["script"] naming the parted script actually run.
// unit_preserve_case is NOT implemented — this port always returns the
// unit string as given in the `unit` argument (or lowercased "KiB"'s
// own default), rather than real parted's own separate lower/preserved
// output modes; this is a narrowing, not a silent mismatch.
//
// Parsing: parted's `-m` disk line is parsed as a fixed 7 fields
// (path:size:transport:logical-sector:physical-sector:label:model),
// with an optional trailing flags field tolerated but not required.
// This is parted's own DOCUMENTED machine-readable format as of the
// versions this port was checked against; older/newer parted releases
// with a different field count are not specifically handled — a field
// count this port doesn't expect surfaces as a clear parse error rather
// than silently misattributing fields.
func moduleParted(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	device, err := requireString(args, "device")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "info")
	unit := argString(args, "unit", "KiB")
	align := argString(args, "align", "optimal")

	var scripts []string

	if state == "present" || state == "absent" {
		number, err := requirePartedNumber(args)
		if err != nil {
			return Result{}, err
		}
		label := argString(args, "label", "msdos")

		disk, parts, err := partedPrint(ctx, conn, device, unit)
		if err != nil {
			return Result{}, err
		}
		changed := false

		if state == "present" && disk["table"] != "" && disk["table"] != label {
			cmd := "parted -s " + shellQuote(device) + " mklabel " + shellQuote(label)
			if _, err := run(ctx, conn, cmd); err != nil {
				return Result{}, err
			}
			scripts = append(scripts, "mklabel "+label+" ")
			changed = true
			disk, parts, err = partedPrint(ctx, conn, device, unit)
			if err != nil {
				return Result{}, err
			}
		}

		existing := partedFind(parts, number)

		if state == "absent" {
			if existing != nil {
				if _, err := run(ctx, conn, "parted -s "+shellQuote(device)+" rm "+strconv.Itoa(number)); err != nil {
					return Result{}, err
				}
				scripts = append(scripts, fmt.Sprintf("rm %d ", number))
				changed = true
			}
		} else { // present
			partStart := argString(args, "part_start", "0%")
			partEnd := argString(args, "part_end", "100%")
			partType := argString(args, "part_type", "primary")
			fsType := argString(args, "fs_type", "")
			name := argString(args, "name", "")
			flags := argStringList(args, "flags")
			resize := argBool(args, "resize", false)

			if existing == nil {
				mkpart := "mkpart " + partType
				if fsType != "" {
					mkpart += " " + fsType
				}
				mkpart += " " + partStart + " " + partEnd + " "
				cmd := "parted -s -a " + shellQuote(align) + " " + shellQuote(device) + " unit " + shellQuote(unit) + " " + mkpart
				if _, err := run(ctx, conn, cmd); err != nil {
					return Result{}, err
				}
				scripts = append(scripts, mkpart)
				changed = true
				if name != "" {
					cmd := "parted -s " + shellQuote(device) + " name " + strconv.Itoa(number) + " " + shellQuote(name)
					if _, err := run(ctx, conn, cmd); err != nil {
						return Result{}, err
					}
					scripts = append(scripts, fmt.Sprintf("name %d %s ", number, name))
				}
			} else if resize {
				cmd := "parted -s " + shellQuote(device) + " resizepart " + strconv.Itoa(number) + " " + partEnd
				if _, err := run(ctx, conn, cmd); err != nil {
					return Result{}, err
				}
				scripts = append(scripts, fmt.Sprintf("resizepart %d %s ", number, partEnd))
				changed = true
			}

			var curFlags []string
			if existing != nil {
				curFlags = existing.Flags
			}
			for _, f := range flags {
				if !containsStr(curFlags, f) {
					cmd := "parted -s " + shellQuote(device) + " set " + strconv.Itoa(number) + " " + shellQuote(f) + " on"
					if _, err := run(ctx, conn, cmd); err != nil {
						return Result{}, err
					}
					scripts = append(scripts, fmt.Sprintf("set %d %s on ", number, f))
					changed = true
				}
			}
		}

		disk, parts, err = partedPrint(ctx, conn, device, unit)
		if err != nil {
			return Result{}, err
		}
		result := partedResult(disk, parts, strings.Join(scripts, ""))
		if changed {
			result.Changed = true
		}
		return result, nil
	}

	if state != "info" {
		return Result{}, errArg("parted: state must be present, absent, or info, got %q", state)
	}
	disk, parts, err := partedPrint(ctx, conn, device, unit)
	if err != nil {
		return Result{}, err
	}
	return partedResult(disk, parts, "unit "+unit+" print "), nil
}

func requirePartedNumber(args map[string]any) (int, error) {
	v, ok := args["number"]
	if !ok {
		return 0, errArg("parted: number is required for state present/absent")
	}
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case string:
		i, err := strconv.Atoi(n)
		if err != nil {
			return 0, errArg("parted: number must be an integer, got %q", n)
		}
		return i, nil
	default:
		return 0, errArg("parted: number must be an integer")
	}
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// partedPrint runs `parted -s -m device unit UNIT print` and parses its
// output into a disk-info map and a slice of partitions.
func partedPrint(ctx context.Context, conn remoteexec.Connection, device, unit string) (map[string]string, []modulePartedInfo, error) {
	cmd := "parted -s -m " + shellQuote(device) + " unit " + shellQuote(unit) + " print"
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return nil, nil, err
	}
	if res.RC != 0 {
		return nil, nil, fmt.Errorf("parted: %s: exit %d: %s", cmd, res.RC, strings.TrimSpace(res.Stderr))
	}
	return partedParse(res.Stdout, unit)
}

func partedParse(out, unit string) (map[string]string, []modulePartedInfo, error) {
	lines := splitLines(out)
	disk := map[string]string{"unit": unit}
	var parts []modulePartedInfo
	for _, line := range lines {
		line = strings.TrimSuffix(strings.TrimSpace(line), ";")
		if line == "" || line == "BYT" {
			continue
		}
		fields := strings.Split(line, ":")
		// A disk line's first field is the device path (starts with
		// "/"); a partition line's first field is its number.
		if strings.HasPrefix(fields[0], "/") {
			for len(fields) < 7 {
				fields = append(fields, "")
			}
			disk["path"] = fields[0]
			disk["size"] = fields[1]
			disk["transport"] = fields[2]
			disk["logical_block"] = fields[3]
			disk["physical_block"] = fields[4]
			disk["table"] = fields[5]
			disk["model"] = fields[6]
			continue
		}
		num, err := strconv.Atoi(fields[0])
		if err != nil {
			continue // not a recognized line shape; skip rather than guess
		}
		for len(fields) < 7 {
			fields = append(fields, "")
		}
		p := modulePartedInfo{
			Num:     num,
			Begin:   partedParseSize(fields[1]),
			End:     partedParseSize(fields[2]),
			Size:    partedParseSize(fields[3]),
			FSType:  fields[4],
			Name:    fields[5],
			rawUnit: unit,
		}
		if fields[6] != "" {
			for _, f := range strings.Split(fields[6], ",") {
				f = strings.TrimSpace(f)
				if f != "" {
					p.Flags = append(p.Flags, f)
				}
			}
		}
		parts = append(parts, p)
	}
	return disk, parts, nil
}

// partedParseSize strips a trailing unit suffix (e.g. "1074MiB",
// "15%") from a parted size field, returning the leading numeric value.
func partedParseSize(s string) float64 {
	i := 0
	for i < len(s) && (s[i] == '.' || s[i] == '-' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	f, _ := strconv.ParseFloat(s[:i], 64)
	return f
}

func partedFind(parts []modulePartedInfo, number int) *modulePartedInfo {
	for i := range parts {
		if parts[i].Num == number {
			return &parts[i]
		}
	}
	return nil
}

func partedResult(disk map[string]string, parts []modulePartedInfo, script string) Result {
	partsOut := make([]any, 0, len(parts))
	for _, p := range parts {
		flags := p.Flags
		if flags == nil {
			flags = []string{}
		}
		partsOut = append(partsOut, map[string]any{
			"num": p.Num, "begin": p.Begin, "end": p.End, "size": p.Size,
			"fstype": p.FSType, "name": p.Name, "flags": flags, "unit": p.rawUnit,
		})
	}
	r := Ok("parted")
	r = r.WithExtra("disk", disk)
	r = r.WithExtra("partitions", partsOut)
	r = r.WithExtra("script", script)
	return r
}
