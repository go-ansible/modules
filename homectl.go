package modules

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHomectl implements (a subset of) Ansible's `homectl`
// (community.general) module: creates/updates/removes a
// systemd-homed-managed user account via the `homectl` CLI, feeding it
// a JSON "user record" (systemd's own USER_RECORD format) over stdin —
// read from real homectl.py's own Homectl class (this batch's hard
// rule: several of the field-update rules below are only visible in
// the implementation, not EXAMPLES/OPTIONS, including two verbatim-
// preserved upstream bugs — see below).
//
// Args: name (string, required, aliased user/username); password
// (string, required when state=present) — real homectl also verifies a
// given password against the EXISTING user's own stored hash before
// allowing an update (to give a friendly "password is incorrect"
// failure instead of letting a wrong-password `homectl update` fail
// cryptically); this port does NOT replicate that local pre-check (it
// would require reimplementing passlib's own multi-scheme crypt
// verification, which this port has no library for) — an existing
// user's update is always attempted, and a wrong password surfaces as
// a normal Result{Failed:true} from homectl's own real rejection,
// which is the same practical outcome, just via homectl's own error
// text rather than the Python module's own custom wording; state
// (present|absent, default "present"); storage, homedir, imagepath,
// uid, gid (only applied when CREATING a new user — matches real
// homectl's own restriction, since homed itself only accepts these at
// creation time); disksize (human-size string, e.g. "10G") — converted
// to bytes via this package's own filesize.go unit grammar (a superset
// of real homectl's own human_to_bytes, close enough for the common
// suffixes both accept); resize (bool, default false) — requires
// disksize; realname (aliased comment), realm, email, location,
// iconname, skeleton (aliased skel), shell, umask (int), environment
// (aliased setenv, comma-separated), timezone, memberof (aliased
// groups, comma-separated), passwordhint, sshkeys (comma-separated),
// language, notbefore, notafter (int, Unix epoch seconds), mountopts
// (comma-separated, from nosuid/nodev/noexec).
//
// Two verbatim-preserved upstream bugs (real community.general.homectl,
// not introduced by this port — replicated for functional parity per
// this project's iron rule against silently "fixing" real behavior):
//   - locked: real homectl's own field-update check is `if self.locked:`
//     — a Python truthiness test on a bool. locked=false is just as
//     falsy as locked=None (unset), so real homectl can never actually
//     set locked=false via this argument; only locked=true ever takes
//     effect. This port applies the identical restriction.
//   - language/notbefore/notafter: real homectl's own change-detection
//     for these three fields compares against `self.locked` (the
//     LOCKED argument's own current value) instead of the field's own
//     previous/argument value — an apparent copy-paste bug. Its
//     practical effect: since self.locked (a bool or None) can never
//     equal the stored field's own value (a string or int), providing
//     language/notbefore/notafter ALWAYS reports Changed, never
//     idempotently. This port reproduces the same comparison (against
//     the raw `locked` argument) for the same practical effect.
//
// Similarly, uid/gid/umask/notbefore/notafter are only applied when
// their given value is non-zero (real homectl's own `if self.uid and
// self.gid`/`if self.umask`/etc. truthiness checks silently treat 0 as
// "not given" — matched here rather than "fixed", since a task setting
// one of these to precisely 0 is rare and this is what real homectl
// actually does with it).
//
// Password hashing: real homectl computes a SHA-512-crypt hash via
// passlib (10000 rounds) purely to populate the new user's own
// `privileged.hashedPassword` field at CREATE time (homed derives its
// own authentication hash from the plaintext `secret.password` field
// regardless — this stored hash is closer to a record-completeness
// nicety than a strict functional requirement). This port has no crypt
// library, so — mirroring mail.go's own "shell out to a real external
// tool" stance — it shells out to the target's own `openssl passwd -6
// -stdin -salt rounds=10000$<random salt>`, piping the password over
// stdin (never as a command-line argument).
func moduleHomectl(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("homectl: state must be present or absent, got %q", state)
	}
	password := argString(args, "password", "")
	if state == "present" && password == "" {
		return Result{}, errArg("homectl: password is required when state is present")
	}
	resize := argBool(args, "resize", false)
	disksize := argString(args, "disksize", "")
	if resize && disksize == "" {
		return Result{}, errArg("homectl: resize requires disksize to be set")
	}

	active, err := homectlServiceActive(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	if !active {
		return Fail("systemd-homed.service is not active"), nil
	}

	exists, metadata, _, err := homectlInspect(ctx, conn, name)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok("User does not exist!"), nil
		}
		res, err := runStatus(ctx, conn, "homectl remove "+shellQuote(name))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(strings.TrimSpace(res.Stderr)).WithExtra("rc", res.RC), nil
		}
		return Changed("User "+name+" removed!").WithExtra("rc", 0), nil
	}

	// state == "present"
	if !exists {
		record := homectlBuildCreateRecord(ctx, conn, args, name, password)
		data, err := json.Marshal(record)
		if err != nil {
			return Result{}, err
		}
		res, err := conn.Exec(ctx, "homectl create --identity=-", strings.NewReader(string(data)))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(strings.TrimSpace(res.Stderr)).WithExtra("rc", res.RC), nil
		}
		_, newMetadata, raw, err := homectlInspect(ctx, conn, name)
		if err != nil {
			return Result{}, err
		}
		result := Changed("User "+name+" created!").WithExtra("rc", 0)
		if newMetadata != nil {
			result = result.WithExtra("data", newMetadata)
		} else {
			result = result.WithExtra("data", raw)
		}
		return result, nil
	}

	newRecord, changed := homectlBuildModifyRecord(metadata, args, name, password)
	if resize && disksize != "" {
		changed = true
	}
	if changed {
		data, err := json.Marshal(newRecord)
		if err != nil {
			return Result{}, err
		}
		cmd := "homectl update " + shellQuote(name) + " --identity=-"
		if resize && disksize != "" {
			cmd += " --and-resize true"
		}
		res, err := conn.Exec(ctx, cmd, strings.NewReader(string(data)))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(strings.TrimSpace(res.Stderr)).WithExtra("rc", res.RC), nil
		}
	}

	_, finalMetadata, raw, err := homectlInspect(ctx, conn, name)
	if err != nil {
		return Result{}, err
	}
	msg := "User " + name + " unchanged"
	result := Ok(msg)
	if changed {
		result = Changed("User " + name + " modified")
	}
	result = result.WithExtra("rc", 0)
	if finalMetadata != nil {
		result = result.WithExtra("data", finalMetadata)
	} else {
		result = result.WithExtra("data", raw)
	}
	return result, nil
}

// homectlServiceActive reports whether systemd-homed.service is
// active. Matching a real (if odd) upstream detail: a non-zero
// `systemctl show` exit leaves this reporting true (real homed_
// service_active's own is_active starts True and is only ever flipped
// False inside its rc==0 branch) — so a systemctl failure here does
// NOT itself block the module; only an explicit non-"active" state
// does.
func homectlServiceActive(ctx context.Context, conn remoteexec.Connection) (bool, error) {
	res, err := runStatus(ctx, conn, "systemctl show systemd-homed.service -p ActiveState")
	if err != nil {
		return false, err
	}
	if res.RC != 0 {
		return true, nil
	}
	_, val, ok := strings.Cut(strings.TrimSpace(res.Stdout), "=")
	if !ok {
		return true, nil
	}
	return strings.TrimSpace(val) == "active", nil
}

// homectlInspect runs `homectl inspect <name> -j --no-pager` and
// parses its JSON output. exists reports whether the command succeeded
// (RC 0); metadata is nil if exists is false or the JSON failed to
// parse (err is set in the latter case, not the former, matching
// real homectl's own user_exists — a non-zero exit just means "no such
// user", not an error).
func homectlInspect(ctx context.Context, conn remoteexec.Connection, name string) (exists bool, metadata map[string]any, raw string, err error) {
	res, err := runStatus(ctx, conn, "homectl inspect "+shellQuote(name)+" -j --no-pager")
	if err != nil {
		return false, nil, "", err
	}
	if res.RC != 0 {
		return false, nil, "", nil
	}
	var m map[string]any
	if uerr := json.Unmarshal([]byte(res.Stdout), &m); uerr != nil {
		return true, nil, res.Stdout, nil
	}
	return true, m, res.Stdout, nil
}

// homectlHashPassword computes a SHA-512-crypt hash of password via
// the target's own `openssl passwd -6`, with a random 16-character
// crypt-alphabet salt and 10000 rounds (matching real homectl's own
// passlib sha512_crypt parameters). Returns "" if openssl isn't
// present or fails — the caller treats that as "no precomputed hash",
// which is not fatal (see moduleHomectl's own doc comment: this hash
// is a record-completeness nicety, not load-bearing for homed's own
// authentication).
func homectlHashPassword(ctx context.Context, conn remoteexec.Connection, password string) string {
	salt := homectlRandomSalt(16)
	res, err := conn.Exec(ctx, "openssl passwd -6 -stdin -salt "+shellQuote("rounds=10000$"+salt), strings.NewReader(password+"\n"))
	if err != nil || res.RC != 0 {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
}

const homectlCryptAlphabet = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func homectlRandomSalt(n int) string {
	b := make([]byte, n)
	if _, err := cryptorand.Read(b); err != nil {
		// Extremely unlikely; fall back to a fixed (still valid, just
		// less random) salt rather than failing the whole operation.
		for i := range b {
			b[i] = byte(i)
		}
	}
	out := make([]byte, n)
	for i, v := range b {
		out[i] = homectlCryptAlphabet[int(v)%len(homectlCryptAlphabet)]
	}
	return string(out)
}

// homectlBuildCreateRecord builds the JSON user record for `homectl
// create`, matching real create_json_record(create=True).
func homectlBuildCreateRecord(ctx context.Context, conn remoteexec.Connection, args map[string]any, name, password string) map[string]any {
	record := map[string]any{
		"userName": name,
		"secret":   map[string]any{"password": []string{password}},
	}
	if hash := homectlHashPassword(ctx, conn, password); hash != "" {
		record["privileged"] = map[string]any{"hashedPassword": []string{hash}}
	}

	uid := argInt(args, "uid", 0)
	gid := argInt(args, "gid", 0)
	if uid != 0 && gid != 0 {
		record["uid"] = uid
		record["gid"] = gid
	}
	if v := argString(args, "storage", ""); v != "" {
		record["storage"] = v
	}
	if v := argString(args, "homedir", ""); v != "" {
		record["homeDirectory"] = v
	}
	if v := argString(args, "imagepath", ""); v != "" {
		record["imagePath"] = v
	}
	homectlApplyCommonFields(record, args)
	return record
}

// homectlBuildModifyRecord builds the JSON user record for `homectl
// update`, starting from the user's own existing metadata (stripping
// signature/binding/status/lastChangeUSec, matching real
// create_json_record(create=False)) and applying only the fields given
// in args, reporting whether anything actually changed.
func homectlBuildModifyRecord(existing map[string]any, args map[string]any, name, password string) (map[string]any, bool) {
	record := map[string]any{}
	for k, v := range existing {
		record[k] = v
	}
	delete(record, "signature")
	delete(record, "binding")
	delete(record, "status")
	delete(record, "lastChangeUSec")

	record["userName"] = name
	record["secret"] = map[string]any{"password": []string{password}}

	changed := false
	if v := argString(args, "memberof", ""); v != "" {
		list := strings.Split(v, ",")
		if !homectlStringSliceEqual(record["memberOf"], list) {
			record["memberOf"] = list
			changed = true
		}
	}
	if v := argString(args, "realname", ""); v != "" {
		if record["realName"] != v {
			record["realName"] = v
			changed = true
		}
	}
	if homectlApplyCommonFields(record, args) {
		changed = true
	}
	return record, changed
}

// homectlApplyCommonFields applies the fields shared by create and
// update (everything except uid/gid/storage/homedir/imagepath, which
// are create-only): disksize, realm, email, location, iconname,
// skeleton, shell, umask, environment, timezone, locked, passwordhint,
// sshkeys, language, notbefore, notafter, mountopts. Returns whether it
// changed anything, so the caller (create vs modify) can fold that into
// its own overall Changed decision — create always reports Changed
// regardless, so its own caller ignores this return value.
func homectlApplyCommonFields(record map[string]any, args map[string]any) bool {
	changed := false

	if v := argString(args, "disksize", ""); v != "" {
		if bytes, err := filesizeParseBytes(v, 1); err == nil {
			if !homectlNumEqual(record["diskSize"], bytes) {
				record["diskSize"] = bytes
				changed = true
			}
		}
	}
	if v := argString(args, "realm", ""); v != "" && record["realm"] != v {
		record["realm"] = v
		changed = true
	}
	if v := argString(args, "email", ""); v != "" && record["emailAddress"] != v {
		record["emailAddress"] = v
		changed = true
	}
	if v := argString(args, "location", ""); v != "" && record["location"] != v {
		record["location"] = v
		changed = true
	}
	if v := argString(args, "iconname", ""); v != "" && record["iconName"] != v {
		record["iconName"] = v
		changed = true
	}
	if v := argString(args, "skeleton", ""); v != "" && record["skeletonDirectory"] != v {
		record["skeletonDirectory"] = v
		changed = true
	}
	if v := argString(args, "shell", ""); v != "" && record["shell"] != v {
		record["shell"] = v
		changed = true
	}
	if v := argInt(args, "umask", 0); v != 0 && !homectlNumEqual(record["umask"], int64(v)) {
		record["umask"] = v
		changed = true
	}
	if v := argString(args, "environment", ""); v != "" {
		list := strings.Split(v, ",")
		if !homectlStringSliceEqual(record["environment"], list) {
			record["environment"] = list
			changed = true
		}
	}
	if v := argString(args, "timezone", ""); v != "" && record["timeZone"] != v {
		record["timeZone"] = v
		changed = true
	}
	// locked: real homectl only ever acts when the given value is
	// exactly true (see moduleHomectl's own doc comment).
	if argBool(args, "locked", false) {
		if record["locked"] != true {
			record["locked"] = true
			changed = true
		}
	}
	if v := argString(args, "passwordhint", ""); v != "" {
		priv, _ := record["privileged"].(map[string]any)
		if priv == nil {
			priv = map[string]any{}
		}
		if priv["passwordHint"] != v {
			priv["passwordHint"] = v
			record["privileged"] = priv
			changed = true
		}
	}
	if v := argString(args, "sshkeys", ""); v != "" {
		priv, _ := record["privileged"].(map[string]any)
		if priv == nil {
			priv = map[string]any{}
		}
		list := strings.Split(v, ",")
		if !homectlStringSliceEqual(priv["sshAuthorizedKeys"], list) {
			priv["sshAuthorizedKeys"] = list
			record["privileged"] = priv
			changed = true
		}
	}
	// language/notbefore/notafter: verbatim-preserved upstream bug —
	// compared against the `locked` argument's own raw value, not the
	// field being set, which in practice always reports Changed when
	// given (see moduleHomectl's own doc comment).
	lockedRaw := args["locked"]
	if v := argString(args, "language", ""); v != "" {
		if !homectlRawEqual(lockedRaw, record["preferredLanguage"]) {
			record["preferredLanguage"] = v
			changed = true
		}
	}
	if v := argInt(args, "notbefore", 0); v != 0 {
		if !homectlRawEqual(lockedRaw, record["notBeforeUSec"]) {
			record["notBeforeUSec"] = v
			changed = true
		}
	}
	if v := argInt(args, "notafter", 0); v != 0 {
		if !homectlRawEqual(lockedRaw, record["notAfterUSec"]) {
			record["notAfterUSec"] = v
			changed = true
		}
	}
	if v := argString(args, "mountopts", ""); v != "" {
		opts := strings.Split(v, ",")
		want := map[string]bool{"nosuid": false, "nodev": false, "noexec": false}
		for _, o := range opts {
			want[strings.TrimSpace(o)] = true
		}
		if !homectlBoolEqual(record["mountNoSuid"], want["nosuid"]) {
			record["mountNoSuid"] = want["nosuid"]
			changed = true
		}
		if !homectlBoolEqual(record["mountNoDevices"], want["nodev"]) {
			record["mountNoDevices"] = want["nodev"]
			changed = true
		}
		if !homectlBoolEqual(record["mountNoExecute"], want["noexec"]) {
			record["mountNoExecute"] = want["noexec"]
			changed = true
		}
	}
	return changed
}

func homectlBoolEqual(a any, b bool) bool {
	ab, ok := a.(bool)
	return ok && ab == b
}

func homectlNumEqual(a any, b int64) bool {
	switch v := a.(type) {
	case float64:
		return int64(v) == b
	case int:
		return int64(v) == b
	case int64:
		return v == b
	default:
		return false
	}
}

func homectlRawEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	af, aok := a.(float64)
	bf, bok := b.(float64)
	if aok && bok {
		return af == bf
	}
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok {
		return as == bs
	}
	ab, aok := a.(bool)
	bb, bok := b.(bool)
	if aok && bok {
		return ab == bb
	}
	return false
}

func homectlStringSliceEqual(a any, b []string) bool {
	list, ok := a.([]any)
	if !ok {
		if sl, ok2 := a.([]string); ok2 {
			list = make([]any, len(sl))
			for i, s := range sl {
				list[i] = s
			}
		} else {
			return false
		}
	}
	if len(list) != len(b) {
		return false
	}
	for i, v := range list {
		s, ok := v.(string)
		if !ok || s != b[i] {
			return false
		}
	}
	return true
}
