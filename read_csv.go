package modules

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleReadCsv implements Ansible's `read_csv` module: fetches a CSV
// file from the target and parses it, read-only — like moduleStat and
// moduleGetent, it never reports Changed.
//
// Args: path (string, required; aliased from `filename`); fieldnames
// ([]string, optional) — column names to use when the file has no
// header row; if omitted, the first row is consumed as the header;
// delimiter (string, optional, a single character; default depends on
// dialect); dialect (excel|excel-tab|unix, default "excel" — this port
// only uses it to pick a default delimiter, tab for excel-tab and comma
// otherwise, since encoding/csv has no notion of Python's csv dialect
// objects beyond delimiter/quoting); skipinitialspace (bool, default
// false); key (string, optional) — column to key the returned `dict` by;
// unique (bool, default true) — fail if `key` is not in fact unique.
//
// Simplifications vs real read_csv: `strict` (real read_csv's strict
// controls Python csv's exception-on-malformed-quoting behavior) is
// accepted but has no effect — Go's encoding/csv always parses quoting
// strictly. What this port DOES relax deliberately is Go's OWN default
// of failing the whole read on a row with a different field count than
// the header (encoding/csv's FieldsPerRecord auto-detection): this port
// always sets FieldsPerRecord=-1 (disabled), so a ragged row doesn't
// hard-fail the entire file — closer to Python's own DictReader, which
// tolerates short/long rows by leaving fields unset/collecting extras.
// A row with fewer fields than the header simply yields a map missing
// those trailing keys; a row with MORE fields than the header has its
// extra fields silently dropped (real DictReader collects them under a
// `restkey`, which this port has no equivalent slot for).
//
// dict's shape when key=="": real read_csv's exact behavior isn't
// re-derivable from its own doc page alone; this port returns an empty
// map rather than guess, so the field is never absent from the result,
// but is only meaningfully populated when key is set — documented here
// as a deliberate, narrow choice rather than a confirmed match to real
// Ansible.
func moduleReadCsv(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := readCsvRequirePath(args)
	if err != nil {
		return Result{}, err
	}
	fieldnames := argStringList(args, "fieldnames")
	dialect := argString(args, "dialect", "excel")
	delimiter := argString(args, "delimiter", "")
	skipInitialSpace := argBool(args, "skipinitialspace", false)
	key := argString(args, "key", "")
	unique := argBool(args, "unique", true)

	data, err := fetchIfExists(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}
	if data == nil {
		return Fail(fmt.Sprintf("could not find or access %q", path)), nil
	}

	r := csv.NewReader(strings.NewReader(string(data)))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = skipInitialSpace
	switch {
	case delimiter != "":
		runes := []rune(delimiter)
		if len(runes) != 1 {
			return Result{}, errArg("read_csv: delimiter must be a single character, got %q", delimiter)
		}
		r.Comma = runes[0]
	case dialect == "excel-tab":
		r.Comma = '\t'
	}

	headers := fieldnames
	if len(headers) == 0 {
		row, err := r.Read()
		if err == io.EOF {
			return Ok(path).WithExtra("list", []any{}).WithExtra("dict", map[string]any{}), nil
		}
		if err != nil {
			return Fail(fmt.Sprintf("read_csv: reading header of %s: %v", path, err)), nil
		}
		headers = row
	}

	var list []any
	dict := map[string]any{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Fail(fmt.Sprintf("read_csv: reading %s: %v", path, err)), nil
		}
		rec := map[string]any{}
		for i, h := range headers {
			if i < len(row) {
				rec[h] = row[i]
			}
		}
		list = append(list, rec)

		if key != "" {
			kv, _ := rec[key].(string)
			if _, dup := dict[kv]; dup && unique {
				return Fail(fmt.Sprintf("read_csv: key %q is not unique for value %q", key, kv)), nil
			}
			dict[kv] = rec
		}
	}
	if list == nil {
		list = []any{}
	}

	return Ok(path).WithExtra("list", list).WithExtra("dict", dict), nil
}

func readCsvRequirePath(args map[string]any) (string, error) {
	if s, ok := args["path"].(string); ok && s != "" {
		return s, nil
	}
	if s, ok := args["filename"].(string); ok && s != "" {
		return s, nil
	}
	return "", errArg("read_csv: path (or its alias filename) is required")
}
