package modules

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRedhatSubscription implements Ansible's `redhat_subscription`
// (community.general) module: registers or unregisters a system with
// Red Hat Subscription Management, via the `subscription-manager` CLI.
//
// Deviation from real redhat_subscription — CLI only, never D-Bus:
// starting from community.general 6.5.0, real redhat_subscription
// prefers registering over D-Bus (talking to the `rhsm` D-Bus service
// subscription-manager itself exposes), specifically so credentials
// never appear as command-line arguments (and therefore never show up
// in the target's own process listing, `ps`); it falls back to the
// `subscription-manager register` CLI only when D-Bus is unavailable,
// unsupported by the distro version, a token or environment is
// involved, or otherwise excluded by real redhat_subscription's own
// _has_dbus_interface/_can_connect_to_dbus checks. This port has no
// D-Bus client wired into remoteexec.Connection (D-Bus is a local IPC
// mechanism, not something reachable by shelling a single command
// string to a remote target the way this port's whole architecture
// works — see module.go's own doc comment on that architectural
// limit), so it ALWAYS uses the CLI path — meaning username/password/
// activationkey/token DO appear as command-line arguments in the
// target's process listing for the duration of the register call, the
// exact exposure real redhat_subscription's own D-Bus path was added
// to eliminate. This is a real, security-relevant behavior gap
// documented here rather than glossed over.
//
// Args: state (present|absent, default present); username; password;
// token; activationkey; org_id (required when activationkey is given,
// matching real redhat_subscription's own check); environment;
// pool_ids ([]any, default [] — each entry either a plain pool-ID
// string, or a single-key {pool_id: quantity} map, matching real
// redhat_subscription's own accepted shapes); consumer_type;
// consumer_name; consumer_id; force_register (bool, default false);
// release; auto_attach (bool); server_hostname, server_insecure,
// server_prefix, server_port, rhsm_baseurl, rhsm_repo_ca_cert,
// server_proxy_hostname, server_proxy_scheme, server_proxy_port,
// server_proxy_user, server_proxy_password — passed to `subscription-
// manager config` before registering, matching real redhat_
// subscription's own rhsm.configure(**module.params) (every key
// matching `^(server|rhsm)_` becomes `--server.x=value` /
// `--rhsm.x=value`, the first underscore only replaced by a dot); the
// module.params keys are still forwarded to config exactly as real
// redhat_subscription does, even though real redhat_subscription's own
// main() no longer reads several of them (server_insecure,
// server_prefix, server_port, rhsm_baseurl, rhsm_repo_ca_cert, every
// server_proxy_* key) into a named local variable — see this port's
// own rhsmSubConfigure for the exact key set. syspurpose (dict, keys
// role/usage/service_level_agreement/addons/sync).
//
// Requires root (checked via `id -u` on the target, the same
// substitution rhsm_release.go/rhsm_repository.go document for their
// own `os.getuid() != 0` checks).
//
// Args this port does NOT implement functionality for (accepted but
// have no effect, exactly like in real redhat_subscription's own
// current main(), whose comments literally read "TODO - no longer
// used?" next to each): none beyond what real redhat_subscription
// itself has already stopped using — this port does not add any
// further unimplemented args of its own.
func moduleRedhatSubscription(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	uid, err := run(ctx, conn, "id -u")
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(uid) != "0" {
		return Fail("redhat_subscription: interacting with subscription-manager requires root permissions ('become: true')"), nil
	}

	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("redhat_subscription: state must be present or absent, got %q", state)
	}
	username := argString(args, "username", "")
	token := argString(args, "token", "")
	activationkey := argString(args, "activationkey", "")
	orgID := argString(args, "org_id", "")
	if activationkey != "" && orgID == "" {
		return Fail("redhat_subscription: org_id is required when using activationkey"), nil
	}
	serverHostname := argString(args, "server_hostname", "")

	poolIDs, err := rhsmParsePoolIDs(args["pool_ids"])
	if err != nil {
		return Result{}, err
	}

	syspurposeChanged := false
	if raw, ok := args["syspurpose"].(map[string]any); ok {
		changed, failMsg, err := rhsmSubUpdateSyspurpose(ctx, conn, raw)
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail("redhat_subscription: failed to update syspurpose attributes: " + failMsg), nil
		}
		syspurposeChanged = changed
	}
	syncSyspurpose := false
	if raw, ok := args["syspurpose"].(map[string]any); ok {
		syncSyspurpose = argBool(raw, "sync", false)
	}

	if state == "absent" {
		registered, err := rhsmSubIsRegistered(ctx, conn)
		if err != nil {
			return Result{}, err
		}
		if !registered {
			return Ok("System already unregistered."), nil
		}
		if failMsg, err := rhsmSubUnregister(ctx, conn); err != nil {
			return Result{}, err
		} else if failMsg != "" {
			return Fail("redhat_subscription: failed to unregister: " + failMsg), nil
		}
		return Changed("System successfully unregistered from " + serverHostname + "."), nil
	}

	// state == "present"
	forceRegister := argBool(args, "force_register", false)
	wasRegistered, err := rhsmSubIsRegistered(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	if wasRegistered && !forceRegister {
		if syncSyspurpose {
			_, _ = runStatus(ctx, conn, "subscription-manager status")
		}
		if len(poolIDs) > 0 {
			changed, subscribed, unsubscribed, failMsg, err := rhsmSubUpdateSubscriptionsByPoolIDs(ctx, conn, poolIDs)
			if err != nil {
				return Result{}, err
			}
			if failMsg != "" {
				return Fail("redhat_subscription: failed to update subscriptions for '" + serverHostname + "': " + failMsg), nil
			}
			r := Ok("")
			r.Changed = changed
			return r.WithExtra("subscribed_pool_ids", subscribed).WithExtra("unsubscribed_serials", unsubscribed), nil
		}
		if syspurposeChanged {
			return Changed("Syspurpose attributes changed."), nil
		}
		return Ok("System already registered."), nil
	}

	if username == "" && activationkey == "" && token == "" {
		return Fail("redhat_subscription: state is present but any of the following are missing: username, activationkey, token"), nil
	}

	// enable(): remove any existing redhat.repo (real redhat_subscription's
	// own Rhsm.enable() also toggles the rhnplugin/subscription-manager
	// yum plugin .conf files' [main] enabled flag via configparser; this
	// port does not reproduce that part — see the package-level note
	// below for why.
	if exists, err := pathExists(ctx, conn, "/etc/yum.repos.d/redhat.repo"); err != nil {
		return Result{}, err
	} else if exists {
		if _, err := run(ctx, conn, "rm -f /etc/yum.repos.d/redhat.repo"); err != nil {
			return Result{}, err
		}
	}

	if err := rhsmSubConfigure(ctx, conn, args); err != nil {
		return Result{}, err
	}

	if failMsg, err := rhsmSubRegisterCLI(ctx, conn, args); err != nil {
		return Result{}, err
	} else if failMsg != "" {
		return Fail("redhat_subscription: failed to register with '" + serverHostname + "': " + failMsg), nil
	}

	if syncSyspurpose {
		_, _ = runStatus(ctx, conn, "subscription-manager status")
	}

	var subscribedPoolIDs any = map[string]any{}
	if len(poolIDs) > 0 {
		subscribed, failMsg, err := rhsmSubSubscribeByPoolIDs(ctx, conn, poolIDs)
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail("redhat_subscription: failed to register with '" + serverHostname + "': " + failMsg), nil
		}
		subscribedPoolIDs = subscribed
	}

	return Changed("System successfully registered to '"+serverHostname+"'.").
		WithExtra("subscribed_pool_ids", subscribedPoolIDs), nil
}

// rhsmSubIsRegistered reports whether the target is currently
// registered, via `subscription-manager identity`'s own exit code —
// matching real redhat_subscription's own Rhsm.is_registered.
func rhsmSubIsRegistered(ctx context.Context, conn remoteexec.Connection) (bool, error) {
	res, err := runStatus(ctx, conn, "subscription-manager identity")
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

// rhsmSubConfigure runs `subscription-manager config --server.x=value
// ...` for every server_*/rhsm_* argument that is set, matching real
// redhat_subscription's own Rhsm.configure — including the keys real
// redhat_subscription's own main() no longer reads into a named local
// variable (server_insecure, server_prefix, server_port, rhsm_baseurl,
// rhsm_repo_ca_cert, and every server_proxy_* key): they are still
// present in module.params, which is exactly what real
// redhat_subscription passes to configure(**module.params). Keys are
// processed in sorted order, matching real Rhsm.configure's own
// `sorted(kwargs.items())` (config's own generated command-line order,
// not the outcome, since each --server.x=value is independent).
func rhsmSubConfigure(ctx context.Context, conn remoteexec.Connection, args map[string]any) error {
	keys := []string{
		"rhsm_baseurl", "rhsm_repo_ca_cert",
		"server_hostname", "server_insecure", "server_prefix", "server_port",
		"server_proxy_hostname", "server_proxy_password", "server_proxy_port",
		"server_proxy_scheme", "server_proxy_user",
	}
	sort.Strings(keys)
	var options []string
	for _, k := range keys {
		v, ok := args[k]
		if !ok || v == nil {
			continue
		}
		s := fmt.Sprint(v)
		if s == "" {
			continue
		}
		flag := strings.Replace(k, "_", ".", 1)
		options = append(options, "--"+flag+"="+s)
	}
	if len(options) == 0 {
		return nil
	}
	cmd := "subscription-manager config"
	for _, o := range options {
		cmd += " " + shellQuote(o)
	}
	_, err := run(ctx, conn, cmd)
	return err
}

// rhsmSubRegisterCLI builds and runs `subscription-manager register
// ...`, matching real redhat_subscription's own
// Rhsm._register_using_cli exactly (flag order included).
func rhsmSubRegisterCLI(ctx context.Context, conn remoteexec.Connection, args map[string]any) (failMsg string, err error) {
	cmdArgs := []string{"register"}
	if argBool(args, "force_register", false) {
		cmdArgs = append(cmdArgs, "--force")
	}
	if v := argString(args, "org_id", ""); v != "" {
		cmdArgs = append(cmdArgs, "--org", v)
	}
	if argBool(args, "auto_attach", false) {
		cmdArgs = append(cmdArgs, "--auto-attach")
	}
	if v := argString(args, "consumer_type", ""); v != "" {
		cmdArgs = append(cmdArgs, "--type", v)
	}
	if v := argString(args, "consumer_name", ""); v != "" {
		cmdArgs = append(cmdArgs, "--name", v)
	}
	if v := argString(args, "consumer_id", ""); v != "" {
		cmdArgs = append(cmdArgs, "--consumerid", v)
	}
	if v := argString(args, "environment", ""); v != "" {
		cmdArgs = append(cmdArgs, "--environment", v)
	}
	activationkey := argString(args, "activationkey", "")
	token := argString(args, "token", "")
	switch {
	case activationkey != "":
		cmdArgs = append(cmdArgs, "--activationkey", activationkey)
	case token != "":
		cmdArgs = append(cmdArgs, "--token", token)
	default:
		if v := argString(args, "username", ""); v != "" {
			cmdArgs = append(cmdArgs, "--username", v)
		}
		if v := argString(args, "password", ""); v != "" {
			cmdArgs = append(cmdArgs, "--password", v)
		}
	}
	if v := argString(args, "release", ""); v != "" {
		cmdArgs = append(cmdArgs, "--release", v)
	}

	quoted := make([]string, len(cmdArgs))
	for i, a := range cmdArgs {
		quoted[i] = shellQuote(a)
	}
	res, err := conn.Exec(ctx, "subscription-manager "+strings.Join(quoted, " "), nil)
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return strings.TrimSpace(res.Stderr), nil
	}
	return "", nil
}

// rhsmSubUnregister runs `subscription-manager unregister`. Deviation:
// real redhat_subscription's own Rhsm.unregister also disables the
// rhnplugin/subscription-manager yum plugin .conf files afterward
// (the same update_plugin_conf as enable(), see moduleRedhatSubscription's
// own doc comment on why this port skips that step).
func rhsmSubUnregister(ctx context.Context, conn remoteexec.Connection) (failMsg string, err error) {
	res, err := runStatus(ctx, conn, "subscription-manager unregister")
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return strings.TrimSpace(res.Stderr), nil
	}
	return "", nil
}

// rhsmParsePoolIDs normalizes the pool_ids argument (a list whose
// entries are either a plain pool-ID string, or a single-key
// {pool_id: quantity} map) into pool ID -> quantity (nil meaning "no
// explicit quantity given"), matching real redhat_subscription's own
// main() normalization loop.
func rhsmParsePoolIDs(raw any) (map[string]*int, error) {
	list, ok := raw.([]any)
	if !ok {
		return nil, nil
	}
	out := map[string]*int{}
	for _, item := range list {
		switch v := item.(type) {
		case string:
			out[v] = nil
		case map[string]any:
			if len(v) != 1 {
				return nil, errArg("redhat_subscription: unable to parse pool_ids option")
			}
			for k, qv := range v {
				n := argInt(map[string]any{"q": qv}, "q", 0)
				out[k] = &n
			}
		default:
			out[fmt.Sprint(v)] = nil
		}
	}
	return out, nil
}

// rhsmSubListPools runs `subscription-manager list <flag>` (--available
// or --consumed) and parses its ":"-separated key/value block output
// into one map per pool/subscription, grouped by each "Product Name:"/
// "Subscription Name:" line starting a new record — matching real
// redhat_subscription's own RhsmPools._load_product_list exactly,
// including its own literal attribute names (PoolId/PoolID,
// QuantityUsed, Serial): this port cannot verify the exact column
// labels subscription-manager's own CLI prints without a live RHEL
// system to check against (a hard rule of this batch is not to guess
// unverifiable output shapes), so it reuses the SAME key names real
// redhat_subscription's own source already assumes, rather than
// inventing different ones — if real redhat_subscription's own
// attribute lookups are wrong for a given subscription-manager
// version, so are this port's, identically.
func rhsmSubListPools(ctx context.Context, conn remoteexec.Connection, flag string) (records []map[string]string, failMsg string, err error) {
	res, err := runStatus(ctx, conn, "subscription-manager list "+flag)
	if err != nil {
		return nil, "", err
	}
	if res.RC != 0 {
		return nil, fmt.Sprintf("subscription-manager list %s: exit %d: %s", flag, res.RC, strings.TrimSpace(res.Stderr)), nil
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ReplaceAll(strings.TrimSpace(key), " ", "")
		value = strings.TrimSpace(value)
		if key == "ProductName" || key == "SubscriptionName" {
			records = append(records, map[string]string{key: value})
			continue
		}
		if len(records) > 0 {
			records[len(records)-1][key] = value
		}
	}
	return records, "", nil
}

func rhsmPoolID(rec map[string]string) string {
	if v, ok := rec["PoolId"]; ok {
		return v
	}
	return rec["PoolID"]
}

// rhsmSubSubscribeByPoolIDs attaches every pool ID in poolIDs (in
// sorted order, matching real redhat_subscription's own
// `sorted(pool_ids.items())`) via `subscription-manager attach --pool
// <id> [--quantity <n>]`, after checking each is actually available —
// matching real redhat_subscription's own subscribe_by_pool_ids.
func rhsmSubSubscribeByPoolIDs(ctx context.Context, conn remoteexec.Connection, poolIDs map[string]*int) (map[string]any, string, error) {
	available, failMsg, err := rhsmSubListPools(ctx, conn, "--available")
	if err != nil {
		return nil, "", err
	}
	if failMsg != "" {
		return nil, failMsg, nil
	}
	availableIDs := map[string]bool{}
	for _, rec := range available {
		availableIDs[rhsmPoolID(rec)] = true
	}

	var keys []string
	for k := range poolIDs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, id := range keys {
		if !availableIDs[id] {
			return nil, "Pool ID: " + id + " not in list of available pools", nil
		}
		cmdArgs := []string{"attach", "--pool", id}
		if q := poolIDs[id]; q != nil {
			cmdArgs = append(cmdArgs, "--quantity", strconv.Itoa(*q))
		}
		quoted := make([]string, len(cmdArgs))
		for i, a := range cmdArgs {
			quoted[i] = shellQuote(a)
		}
		res, err := conn.Exec(ctx, "subscription-manager "+strings.Join(quoted, " "), nil)
		if err != nil {
			return nil, "", err
		}
		if res.RC != 0 {
			return nil, strings.TrimSpace(res.Stderr), nil
		}
	}

	out := map[string]any{}
	for id, q := range poolIDs {
		if q != nil {
			out[id] = strconv.Itoa(*q)
		} else {
			out[id] = nil
		}
	}
	return out, "", nil
}

// rhsmSubUpdateSubscriptionsByPoolIDs reconciles the target's currently
// consumed pools against poolIDs, matching real redhat_subscription's
// own update_subscriptions_by_pool_ids: any consumed pool not in
// poolIDs (or in poolIDs with a DIFFERENT explicit quantity) is
// unsubscribed by serial, then subscribe_by_pool_ids fills in whatever
// is still missing.
func rhsmSubUpdateSubscriptionsByPoolIDs(ctx context.Context, conn remoteexec.Connection, poolIDs map[string]*int) (changed bool, subscribed, unsubscribedSerials []string, failMsg string, err error) {
	consumed, failMsg, err := rhsmSubListPools(ctx, conn, "--consumed")
	if err != nil {
		return false, nil, nil, "", err
	}
	if failMsg != "" {
		return false, nil, nil, failMsg, nil
	}

	existing := map[string]int{}
	var serialsToRemove []string
	for _, rec := range consumed {
		id := rhsmPoolID(rec)
		used, _ := strconv.Atoi(rec["QuantityUsed"])
		existing[id] = used

		quantity, has := poolIDs[id]
		if !has {
			serialsToRemove = append(serialsToRemove, rec["Serial"])
			continue
		}
		if quantity != nil && *quantity != used {
			serialsToRemove = append(serialsToRemove, rec["Serial"])
		}
	}

	if len(serialsToRemove) > 0 {
		cmdArgs := []string{"remove"}
		for _, s := range serialsToRemove {
			cmdArgs = append(cmdArgs, "--serial="+s)
		}
		quoted := make([]string, len(cmdArgs))
		for i, a := range cmdArgs {
			quoted[i] = shellQuote(a)
		}
		res, err := conn.Exec(ctx, "subscription-manager "+strings.Join(quoted, " "), nil)
		if err != nil {
			return false, nil, nil, "", err
		}
		if res.RC != 0 {
			return false, nil, nil, strings.TrimSpace(res.Stderr), nil
		}
	}

	missing := map[string]*int{}
	var missingKeys []string
	for id, quantity := range poolIDs {
		used := existing[id] // 0 if absent
		if quantity == nil && used == 0 {
			missing[id] = nil
			missingKeys = append(missingKeys, id)
		} else if quantity != nil && *quantity != 0 && *quantity != used {
			missing[id] = quantity
			missingKeys = append(missingKeys, id)
		}
	}
	sort.Strings(missingKeys)

	if len(missing) > 0 {
		_, failMsg, err := rhsmSubSubscribeByPoolIDs(ctx, conn, missing)
		if err != nil {
			return false, nil, nil, "", err
		}
		if failMsg != "" {
			return false, nil, nil, failMsg, nil
		}
	}

	changed = len(missing) > 0 || len(serialsToRemove) > 0
	return changed, missingKeys, serialsToRemove, "", nil
}

// rhsmSyspurposeAllowed lists the syspurpose keys written to
// syspurpose.json — every real redhat_subscription
// SysPurpose.ALLOWED_ATTRIBUTES entry except "sync", which controls
// whether this port (like real redhat_subscription) also runs
// `subscription-manager status` to push the file to the server, but is
// never itself written to the file.
var rhsmSyspurposeAllowed = []string{"role", "usage", "service_level_agreement", "addons"}

const rhsmSyspurposeFile = "/etc/rhsm/syspurpose/syspurpose.json"

// rhsmSubUpdateSyspurpose merges syspurpose (the module's own
// `syspurpose` argument) into the target's
// /etc/rhsm/syspurpose/syspurpose.json, matching real
// redhat_subscription's own SysPurpose.update_syspurpose: any allowed
// attribute present in syspurpose overwrites the file's current value;
// any allowed attribute ABSENT from syspurpose is deleted from the
// file if present; any non-allowed key already in the file (custom,
// user-added content) is left untouched. A non-empty failMsg means
// real redhat_subscription would raise here (an unrecognized
// syspurpose key, or the existing file's content is not valid JSON).
func rhsmSubUpdateSyspurpose(ctx context.Context, conn remoteexec.Connection, syspurpose map[string]any) (changed bool, failMsg string, err error) {
	newValues := map[string]any{}
	for k, v := range syspurpose {
		if k == "sync" {
			continue
		}
		if !containsStr(rhsmSyspurposeAllowed, k) {
			return false, fmt.Sprintf("Attribute: %s not in list of allowed attributes: %v", k, rhsmSyspurposeAllowed), nil
		}
		if v != nil {
			newValues[k] = v
		}
	}

	res, err := runStatus(ctx, conn, "cat "+shellQuote(rhsmSyspurposeFile))
	current := map[string]any{}
	if err != nil {
		return false, "", err
	}
	if res.RC == 0 && strings.TrimSpace(res.Stdout) != "" {
		if err := json.Unmarshal([]byte(res.Stdout), &current); err != nil {
			return false, err.Error(), nil
		}
	}

	changed = !syspurposeMapsEqual(current, newValues)

	merged := map[string]any{}
	for k, v := range current {
		merged[k] = v
	}
	for k, v := range newValues {
		merged[k] = v
	}
	for _, k := range rhsmSyspurposeAllowed {
		if _, wanted := newValues[k]; !wanted {
			delete(merged, k)
		}
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(merged); err != nil {
		return false, "", err
	}
	content := strings.TrimRight(buf.String(), "\n")

	cmd := "mkdir -p " + shellQuote("/etc/rhsm/syspurpose") + " && cat > " + shellQuote(rhsmSyspurposeFile)
	res, err = conn.Exec(ctx, cmd, strings.NewReader(content))
	if err != nil {
		return false, "", err
	}
	if res.RC != 0 {
		return false, strings.TrimSpace(res.Stderr), nil
	}
	return changed, "", nil
}

// syspurposeMapsEqual compares two syspurpose attribute maps for
// value equality (used only to compute the `changed` flag), comparing
// each value's JSON-marshaled form so scalars, nil, and []any addons
// lists all compare correctly regardless of underlying Go type.
func syspurposeMapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		aj, _ := json.Marshal(av)
		bj, _ := json.Marshal(bv)
		if string(aj) != string(bj) {
			return false
		}
	}
	return true
}
