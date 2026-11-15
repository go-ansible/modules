package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// gandiLiveDNSEndpoint is real _gandi_livedns_api.py's own api_endpoint.
const gandiLiveDNSEndpoint = "https://api.gandi.net/v5/livedns"

// gandiErrorStrings is real's error_strings table, which supplies the
// word in front of the API's own message. A status outside it contributes
// nothing, exactly as real's `.get(status, "")` does.
var gandiErrorStrings = map[int]string{
	400: "Bad request",
	401: "Permission denied",
	404: "Resource not found",
}

// moduleGandiLivedns implements Ansible's `gandi_livedns`
// (community.general) module: it manages one DNS record set through
// Gandi's LiveDNS v5 API.
//
// Args: domain (required); record (required — the name, where "" means
// the zone apex and is sent as "@"); type (required, the RR type);
// values (list of strings); ttl (int); state (present|absent, default
// present); personal_access_token or api_key, exactly one of the two.
//
// `values` and `ttl` are required when state is present, and the two
// credentials are mutually exclusive and one is required — real's own
// required_if / mutually_exclusive / required_one_of, with its messages.
//
// A record set is addressed by name AND type, so one task owns one
// (record, type) pair and `values` is the whole content of it. Comparison
// is by SET, as real's is: reordering the values of an A record is not a
// change, and neither is repeating one.
//
// # state: absent removes values, not only records
//
// Giving `values` to an absent task subtracts them. If some of the
// current values survive the subtraction the record set is UPDATED to
// the remainder; only when nothing survives, or when no values were
// given at all, is the set deleted. That asymmetry is real's and it is
// easy to miss from the module's documentation, which says only
// "whether the record(s) should exist or not".
//
// # ⛔ Two deliberate divergences from real
//
// **A check-mode run here never writes.** Real's delete path reaches
// `update_record` on the subtract-to-a-remainder branch with no
// check_mode guard at all (`_gandi_livedns_api.py`, delete_dns_record):
// the PUT is sent, and a `--check` run therefore edits a live zone. The
// guard on the branch below is the fix, and it has its own test. This is
// the same judgement this package already applies to real's
// `wait_for_connection` busy-wait at `sleep: 0` — a defect to decline
// rather than to reproduce, because reproducing it costs the operator
// something real.
//
// **The remainder is sorted.** Real builds it as
// `list(set(current) - set(given))`, and a Python set has no order, so
// the values sent to the API — and the ones the task reports — differ
// run to run for the same inputs. Sorting makes the request a function
// of its arguments. It cannot change which values the record ends up
// with, only the order they are written in, and a DNS record set is
// unordered by definition.
//
// # What a result carries
//
// `record` is the record set as real's build_result renders it: the
// four rrset_* fields under their short names, plus `domain`, with the
// apex coming back as "" rather than "@". It is absent after a deletion,
// which is what real's "returned: success, except on record deletion"
// means.
//
// Real's diff is a before/after pair of those dictionaries. This package
// diffs TEXT, so each side is rendered as indented JSON; the content is
// real's, the presentation is this engine's.
func moduleGandiLivedns(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := requireCurl(ctx, conn, "gandi_livedns", "Gandi LiveDNS"); !ok {
		return res, nil
	}

	apiKey := argString(args, "api_key", "")
	pat := argString(args, "personal_access_token", "")
	switch {
	case apiKey != "" && pat != "":
		return Fail("parameters are mutually exclusive: api_key|personal_access_token"), nil
	case apiKey == "" && pat == "":
		return Fail("one of the following is required: api_key, personal_access_token"), nil
	}

	domain := argString(args, "domain", "")
	if domain == "" {
		return Fail("missing required arguments: domain"), nil
	}
	rrType := argString(args, "type", "")
	if rrType == "" {
		return Fail("missing required arguments: type"), nil
	}
	// `record` is required but "" is a legitimate VALUE for it: the apex.
	// So its presence is what is checked, not its emptiness.
	if _, present := argRaw(args, "record"); !present {
		return Fail("missing required arguments: record"), nil
	}
	record := argString(args, "record", "")

	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Fail(fmt.Sprintf(
			"value of state must be one of: absent, present, got: %s", state)), nil
	}

	_, haveValues := argRaw(args, "values")
	_, haveTTL := argRaw(args, "ttl")
	if state == "present" {
		var missing []string
		if !haveValues {
			missing = append(missing, "values")
		}
		if !haveTTL {
			missing = append(missing, "ttl")
		}
		if len(missing) > 0 {
			return Fail("state is present but all of the following are missing: " +
				strings.Join(missing, ", ")), nil
		}
	}
	values := argStringList(args, "values")
	ttl := argInt(args, "ttl", 0)

	api := &gandiAPI{conn: conn, pat: pat, apiKey: apiKey}

	// Real maps "" to the apex sentinel in both entry points, before it
	// does anything else.
	name := record
	if name == "" {
		name = "@"
	}

	var (
		before, after map[string]any
		changed       bool
		err           error
	)
	if state == "present" {
		before, after, changed, err = api.ensure(ctx, name, rrType, ttl, values, domain, InCheckMode(args))
	} else {
		before, after, changed, err = api.remove(ctx, name, rrType, values, haveValues, domain, InCheckMode(args))
	}
	if err != nil {
		return Fail(err.Error()), nil
	}

	res := Result{Changed: changed, NoMsg: true}
	if after != nil {
		res = res.WithExtra("record", gandiBuildResult(after, domain))
	}
	if InDiffMode(args) {
		res = res.WithDiff(Diff{
			Before:       gandiRenderJSON(gandiBuildResult(before, domain)),
			After:        gandiRenderJSON(gandiBuildResult(after, domain)),
			BeforeHeader: domain + " " + record + " " + rrType,
			AfterHeader:  domain + " " + record + " " + rrType,
		})
	}
	return res, nil
}

// gandiAPI carries the one connection and the one credential, the way
// real's GandiLiveDNSAPI carries its module.
type gandiAPI struct {
	conn   remoteexec.Connection
	pat    string
	apiKey string
}

// call makes one request. errorOn404 is real's own parameter: a 404 is
// an answer ("no such record set") for a lookup and a failure for
// everything else.
func (g *gandiAPI) call(ctx context.Context, path, method, payload string, errorOn404 bool) (any, int, error) {
	// Real: `Bearer <pat>` when a personal access token is set, else
	// `Apikey <api_key>`. The scheme words are the API's, not a choice.
	auth := "Apikey " + g.apiKey
	if g.pat != "" {
		auth = "Bearer " + g.pat
	}

	body, status, err := curlDo(ctx, g.conn, curlReq{
		Method: method,
		URL:    gandiLiveDNSEndpoint + path,
		JSON:   payload,
		Headers: [][2]string{
			{"Authorization", auth},
			{"Content-Type", "application/json"},
		},
	})
	if err != nil {
		return nil, 0, fmt.Errorf("gandi_livedns: %w", err)
	}

	var errMsg string
	if status >= 400 && (status != 404 || errorOn404) {
		errMsg = fmt.Sprintf("API Error %s: %s",
			gandiErrorStrings[status], gandiAPIErrorMessage(body))
	}

	var parsed any
	if strings.TrimSpace(body) != "" {
		if jerr := json.Unmarshal([]byte(body), &parsed); jerr != nil {
			// Real appends to whatever error it already had, so a bad
			// status AND an unparseable body report both.
			errMsg += fmt.Sprintf("; Failed to parse API response with error %v: %s", jerr, body)
		}
	}
	if errMsg != "" {
		return nil, status, fmt.Errorf("%s", errMsg)
	}
	return parsed, status, nil
}

// gandiAPIErrorMessage is real's _build_error_message: the FIRST entry of
// the response's `errors` array, as "<name> :<description>". The space
// before the colon and its absence after are real's own formatting.
func gandiAPIErrorMessage(body string) string {
	var doc struct {
		Errors []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"errors"`
	}
	if json.Unmarshal([]byte(body), &doc) != nil || len(doc.Errors) == 0 {
		return ""
	}
	var s string
	if doc.Errors[0].Name != "" {
		s += doc.Errors[0].Name + " :"
	}
	return s + doc.Errors[0].Description
}

// getRecords looks up the record sets for a name and type.
//
// Real's URL is built up progressively — the type is only appended when
// a name was given — and a 404 means "none", not an error. The API
// answers a single record set with an OBJECT and a collection with an
// ARRAY, so both shapes are normalized here, as real's
// `if not isinstance(records, list)` does.
func (g *gandiAPI) getRecords(ctx context.Context, name, rrType, domain string) ([]map[string]any, error) {
	path := "/domains/" + domain + "/records"
	if name != "" {
		path += "/" + name
		if rrType != "" {
			path += "/" + rrType
		}
	}

	parsed, status, err := g.call(ctx, path, "GET", "", false)
	if err != nil {
		return nil, err
	}
	if status == 404 {
		return nil, nil
	}

	var out []map[string]any
	switch v := parsed.(type) {
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
	case map[string]any:
		out = append(out, v)
	}

	// Real filters by type only when no NAME was given, because the URL
	// already carried the type when one was.
	if name == "" && rrType != "" {
		kept := out[:0:0]
		for _, r := range out {
			if argToString(r["rrset_type"]) == rrType {
				kept = append(kept, r)
			}
		}
		out = kept
	}
	return out, nil
}

func (g *gandiAPI) createRecord(ctx context.Context, name, rrType string, values []string, ttl int, domain string) (map[string]any, error) {
	rec := map[string]any{
		"rrset_name":   name,
		"rrset_type":   rrType,
		"rrset_values": values,
		"rrset_ttl":    ttl,
	}
	payload, err := json.Marshal(rec)
	if err != nil {
		return nil, fmt.Errorf("Failed to encode payload as JSON: %v ", err)
	}
	_, status, err := g.call(ctx, "/domains/"+domain+"/records", "POST", string(payload), true)
	if err != nil {
		return nil, err
	}
	// Real returns the record it SENT, not the API's answer, and only
	// for these two statuses; anything else yields no record at all.
	if status == 200 || status == 201 {
		return rec, nil
	}
	return nil, nil
}

func (g *gandiAPI) updateRecord(ctx context.Context, name, rrType string, values []string, ttl int, domain string) (map[string]any, error) {
	payload, err := json.Marshal(map[string]any{
		"rrset_values": values,
		"rrset_ttl":    ttl,
	})
	if err != nil {
		return nil, fmt.Errorf("Failed to encode payload as JSON: %v ", err)
	}
	parsed, _, err := g.call(ctx, "/domains/"+domain+"/records/"+name+"/"+rrType, "PUT", string(payload), true)
	if err != nil {
		return nil, err
	}
	m, _ := parsed.(map[string]any)
	return m, nil
}

func (g *gandiAPI) deleteRecord(ctx context.Context, name, rrType, domain string) error {
	_, _, err := g.call(ctx, "/domains/"+domain+"/records/"+name+"/"+rrType, "DELETE", "", true)
	return err
}

// ensure is real's ensure_dns_record.
func (g *gandiAPI) ensure(ctx context.Context, name, rrType string, ttl int, values []string, domain string, checkMode bool) (before, after map[string]any, changed bool, err error) {
	records, err := g.getRecords(ctx, name, rrType, domain)
	if err != nil {
		return nil, nil, false, err
	}

	if len(records) > 0 {
		cur := records[0]
		// Real compares the ttl only when one was given, and the values
		// as SETS. Both conditions, so a task that changes only the TTL
		// is a change and a task that reorders values is not.
		doUpdate := false
		if ttl != 0 && gandiInt(cur["rrset_ttl"]) != ttl {
			doUpdate = true
		}
		if values != nil && !gandiSameSet(gandiStrings(cur["rrset_values"]), values) {
			doUpdate = true
		}
		if !doUpdate {
			return cur, cur, false, nil
		}
		if checkMode {
			return cur, gandiSynthetic(name, rrType, values, ttl), true, nil
		}
		if _, err := g.updateRecord(ctx, name, rrType, values, ttl, domain); err != nil {
			return nil, nil, false, err
		}
		// Real re-reads rather than trusting the PUT's answer, so the
		// reported record is the zone's own view of itself.
		records, err = g.getRecords(ctx, name, rrType, domain)
		if err != nil {
			return nil, nil, false, err
		}
		if len(records) == 0 {
			return cur, nil, true, nil
		}
		return cur, records[0], true, nil
	}

	if checkMode {
		return nil, gandiSynthetic(name, rrType, values, ttl), true, nil
	}
	created, err := g.createRecord(ctx, name, rrType, values, ttl, domain)
	if err != nil {
		return nil, nil, false, err
	}
	return nil, created, true, nil
}

// remove is real's delete_dns_record.
//
// haveValues distinguishes "values was not given" from "values was given
// as an empty list": real tests `values is not None`, and an empty list
// subtracts nothing, so every current value survives and the set is
// updated to itself rather than deleted.
func (g *gandiAPI) remove(ctx context.Context, name, rrType string, values []string, haveValues bool, domain string, checkMode bool) (before, after map[string]any, changed bool, err error) {
	records, err := g.getRecords(ctx, name, rrType, domain)
	if err != nil {
		return nil, nil, false, err
	}
	if len(records) == 0 {
		// Real reports no change and no record when there was nothing
		// to remove.
		return nil, nil, false, nil
	}

	cur := records[0]
	current := gandiStrings(cur["rrset_values"])

	if haveValues && values != nil && !gandiSameSet(current, values) {
		remainder := gandiSubtract(current, values)
		if len(remainder) > 0 {
			// ⛔ The guard real does not have. Without it a --check run
			// sends this PUT and edits the zone; see this module's own
			// comment for why that is declined rather than ported.
			if checkMode {
				return cur, gandiSynthetic(name, rrType, remainder, gandiInt(cur["rrset_ttl"])), true, nil
			}
			if _, err := g.updateRecord(ctx, name, rrType, remainder, gandiInt(cur["rrset_ttl"]), domain); err != nil {
				return nil, nil, false, err
			}
			records, err = g.getRecords(ctx, name, rrType, domain)
			if err != nil {
				return nil, nil, false, err
			}
			if len(records) == 0 {
				return cur, nil, true, nil
			}
			return cur, records[0], true, nil
		}
	}

	if !checkMode {
		if err := g.deleteRecord(ctx, name, rrType, domain); err != nil {
			return nil, nil, false, err
		}
	}
	return cur, nil, true, nil
}

// gandiBuildResult is real's build_result: the rrset_* fields under
// their short names, the apex back as "", and the domain added.
//
// A field the API did not send is left out rather than defaulted, which
// is real's `if v is not None`.
func gandiBuildResult(rec map[string]any, domain string) map[string]any {
	if rec == nil {
		return nil
	}
	out := map[string]any{}
	if v, ok := rec["rrset_name"]; ok && v != nil {
		name := argToString(v)
		if name == "@" {
			name = ""
		}
		out["record"] = name
	}
	if v, ok := rec["rrset_type"]; ok && v != nil {
		out["type"] = argToString(v)
	}
	if v, ok := rec["rrset_ttl"]; ok && v != nil {
		out["ttl"] = gandiInt(v)
	}
	if v, ok := rec["rrset_values"]; ok && v != nil {
		out["values"] = gandiStrings(v)
	}
	out["domain"] = domain
	return out
}

// gandiSynthetic is the record a check-mode run reports: what the task
// WOULD have written. Real builds the same four fields inline.
func gandiSynthetic(name, rrType string, values []string, ttl int) map[string]any {
	return map[string]any{
		"rrset_name":   name,
		"rrset_type":   rrType,
		"rrset_values": values,
		"rrset_ttl":    ttl,
	}
}

// gandiRenderJSON renders one side of the diff. An absent side is the
// empty string, which is what this package's callback prints as nothing.
func gandiRenderJSON(m map[string]any) string {
	if m == nil {
		return ""
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return ""
	}
	return string(b) + "\n"
}

// gandiStrings reads an API value list, which arrives as []any of
// strings over JSON.
func gandiStrings(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			out = append(out, argToString(item))
		}
		return out
	}
	return nil
}

// gandiInt reads a TTL, which JSON delivers as float64.
func gandiInt(v any) int { return argIntFromAny(v) }

// gandiSameSet compares by set, as real's `set(a) != set(b)` does:
// order and repetition carry no meaning in a DNS record set.
func gandiSameSet(a, b []string) bool {
	seen := make(map[string]struct{}, len(a))
	for _, s := range a {
		seen[s] = struct{}{}
	}
	other := make(map[string]struct{}, len(b))
	for _, s := range b {
		other[s] = struct{}{}
	}
	if len(seen) != len(other) {
		return false
	}
	for s := range seen {
		if _, ok := other[s]; !ok {
			return false
		}
	}
	return true
}

// gandiSubtract is real's `set(current) - set(given)`, SORTED. Real
// hands the API a Python set's arbitrary order; see the divergence
// noted on the module.
func gandiSubtract(current, remove []string) []string {
	drop := make(map[string]struct{}, len(remove))
	for _, s := range remove {
		drop[s] = struct{}{}
	}
	seen := map[string]struct{}{}
	var out []string
	for _, s := range current {
		if _, gone := drop[s]; gone {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
