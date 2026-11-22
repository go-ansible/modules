package modules

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const credsSecret = "s3cr3t-plaintext-9f2c"

func encryptCmdTable() []struct {
	name string
	args map[string]any
	cmd  string
} {
	return []struct {
		name string
		args map[string]any
		cmd  string
	}{
		{"with_key host", map[string]any{"with_key": "host"}, "systemd-creds encrypt --with-key=host - -"},
		{"with_key tpm2", map[string]any{"with_key": "tpm2"}, "systemd-creds encrypt --with-key=tpm2 - -"},
		{"with_key host+tpm2", map[string]any{"with_key": "host+tpm2"}, "systemd-creds encrypt --with-key=host+tpm2 - -"},
		{"with_key null", map[string]any{"with_key": "null"}, "systemd-creds encrypt --with-key=null - -"},
		{"with_key auto", map[string]any{"with_key": "auto"}, "systemd-creds encrypt --with-key=auto - -"},
		{"with_key auto-initrd", map[string]any{"with_key": "auto-initrd"}, "systemd-creds encrypt --with-key=auto-initrd - -"},
		{"tpm2_device path", map[string]any{"tpm2_device": "/dev/tpmrm0"}, "systemd-creds encrypt --tpm2-device=/dev/tpmrm0 - -"},
		{"tpm2_device auto", map[string]any{"tpm2_device": "auto"}, "systemd-creds encrypt --tpm2-device=auto - -"},
		{"tpm2_pcrs", map[string]any{"tpm2_pcrs": "0+7"}, "systemd-creds encrypt --tpm2-pcrs=0+7 - -"},
		{"tpm2_pcrs empty means none", map[string]any{"tpm2_pcrs": ""}, "systemd-creds encrypt --tpm2-pcrs= - -"},
		{"tpm2_public_key", map[string]any{"tpm2_public_key": "/etc/systemd/tpm2-pcr-public-key.pem"}, "systemd-creds encrypt --tpm2-public-key=/etc/systemd/tpm2-pcr-public-key.pem - -"},
		{"tpm2_public_key_pcrs", map[string]any{"tpm2_public_key_pcrs": "11+12"}, "systemd-creds encrypt --tpm2-public-key-pcrs=11+12 - -"},
		{"all together", map[string]any{
			"name": "db", "with_key": "tpm2", "tpm2_device": "auto", "tpm2_pcrs": "7",
			"tpm2_public_key": "/k.pem", "tpm2_public_key_pcrs": "11",
		}, "systemd-creds encrypt --name=db --with-key=tpm2 --tpm2-device=auto --tpm2-pcrs=7 --tpm2-public-key=/k.pem --tpm2-public-key-pcrs=11 - -"},
	}
}

func TestSystemdCredsEncryptCommandLinePerOption(t *testing.T) {
	for _, tc := range encryptCmdTable() {
		t.Run(tc.name, func(t *testing.T) {
			conn := newFakeConn(map[string]remoteexec.Result{tc.cmd: {Stdout: "CT\n"}})
			args := map[string]any{"secret": credsSecret}
			for k, v := range tc.args {
				args[k] = v
			}
			res, err := moduleSystemdCredsEncrypt(context.Background(), conn, args)
			if err != nil || res.Failed {
				t.Fatalf("res=%+v err=%v", res, err)
			}
			if len(conn.Commands) != 1 || conn.Commands[0] != tc.cmd {
				t.Fatalf("commands = %q, want [%q]", conn.Commands, tc.cmd)
			}
			if res.Extra["value"] != "CT" || res.Changed {
				t.Fatalf("res = %+v", res)
			}
			if conn.Stdins[0] != credsSecret || strings.Contains(conn.Commands[0], credsSecret) {
				t.Fatal("secret must be on stdin only")
			}
		})
	}
}

func TestSystemdCredsDecryptCommandLineTPM2(t *testing.T) {
	cmd := "systemd-creds decrypt --name= --newline=no --tpm2-device=/dev/tpmrm0 --tpm2-signature=/s.json - -"
	_ = cmd
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemd-creds decrypt --tpm2-device=/dev/tpmrm0 --tpm2-signature=/s.json - -": {Stdout: "plain"},
	})
	res, err := moduleSystemdCredsDecrypt(context.Background(), conn, map[string]any{
		"secret": "CT", "tpm2_device": "/dev/tpmrm0", "tpm2_signature": "/s.json",
	})
	if err != nil || res.Failed || res.Extra["value"] != "plain" {
		t.Fatalf("res=%+v err=%v cmds=%q", res, err, conn.Commands)
	}
	for _, bad := range []map[string]any{
		{"secret": "x", "tpm2_device": "/dev/x; reboot"},
		{"secret": "x", "tpm2_signature": "relative.json"},
	} {
		c := newFakeConn(nil)
		if _, err := moduleSystemdCredsDecrypt(context.Background(), c, bad); err == nil || len(c.Commands) != 0 {
			t.Fatalf("%v: err=%v cmds=%q, want refusal before any command", bad, err, c.Commands)
		}
	}
}

func TestSystemdCredsEncryptRefusesHostileValues(t *testing.T) {
	hostile := []string{
		"/dev/tpm0; reboot", "/x$(id)", "/x`id`", "/a b", "relative/path", "/a/../b",
		"/x\nreboot", "/x'y", `/x"y`, "/x|y", "/x&y", "/x>y", "/x*", "-rf", "",
	}
	for _, key := range []string{"tpm2_device", "tpm2_public_key", "tpm2_signature", "dest"} {
		for _, h := range hostile {
			if h == "" {
				continue
			}
			conn := newFakeConn(nil)
			args := map[string]any{"secret": credsSecret, key: h}
			if key == "tpm2_signature" {
				args["dest"] = "/etc/credstore/x"
			}
			if _, err := moduleSystemdCredsEncrypt(context.Background(), conn, args); err == nil {
				t.Errorf("%s=%q accepted", key, h)
			}
			if len(conn.Commands) != 0 {
				t.Errorf("%s=%q: ran %q before refusing", key, h, conn.Commands)
			}
		}
	}
	for _, h := range []string{"7;id", "7 8", "24", "7+7", "+7", "7+", "a", "$(id)", "7\n8", "-1"} {
		for _, key := range []string{"tpm2_pcrs", "tpm2_public_key_pcrs"} {
			conn := newFakeConn(nil)
			if _, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{"secret": "x", key: h}); err == nil || len(conn.Commands) != 0 {
				t.Errorf("%s=%q: err=%v cmds=%q", key, h, err, conn.Commands)
			}
		}
	}
	for _, k := range []string{"tpm2-absent", "HOST", "host; id", "help", "tpm2 ", "$(id)"} {
		conn := newFakeConn(nil)
		if _, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{"secret": "x", "with_key": k}); err == nil || len(conn.Commands) != 0 {
			t.Errorf("with_key=%q accepted", k)
		}
	}
	if _, err := moduleSystemdCredsEncrypt(context.Background(), newFakeConn(nil), map[string]any{"secret": "x", "tpm2_device": "list"}); err == nil {
		t.Error("tpm2_device=list accepted")
	}
}

// The upstream-named options are not validated, only quoted: the quoting
// must hold for a hostile value.
func TestSystemdCredsEncryptQuotesFreeFormValues(t *testing.T) {
	hostile := "a'; rm -rf / #$(id)`id`"
	want := `systemd-creds encrypt '--name=a'"'"'; rm -rf / #$(id)` + "`id`" + `'` + ` - -`
	conn := newFakeConn(map[string]remoteexec.Result{want: {Stdout: "CT"}})
	if _, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{"secret": "x", "name": hostile}); err != nil {
		t.Fatal(err)
	}
	if len(conn.Commands) != 1 || conn.Commands[0] != want {
		t.Fatalf("command = %q, want %q", conn.Commands, want)
	}
	if got := shellQuote("--name=" + hostile); !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
		t.Fatalf("shellQuote left %q unquoted", got)
	}
}

func TestSystemdCredsRequireTPM(t *testing.T) {
	has := "systemd-creds has-tpm2"
	enc := "systemd-creds encrypt --with-key=host+tpm2 - -"
	cases := []struct {
		name     string
		has      remoteexec.Result
		wantFail string
	}{
		{"full", remoteexec.Result{RC: 0, Stdout: "yes\n+firmware\n+driver\n"}, ""},
		{"none", remoteexec.Result{RC: 7, Stdout: "no\n-firmware\n-driver\n"}, "firmware, driver, userspace"},
		{"partial no driver", remoteexec.Result{RC: 2, Stdout: "partial\n+firmware\n-driver\n"}, "missing: driver"},
		{"partial no userspace", remoteexec.Result{RC: 4, Stdout: "partial\n"}, "missing: userspace support"},
		{"verb unknown", remoteexec.Result{RC: 1, Stderr: "Unknown command verb 'has-tpm2'."}, "cannot determine"},
		{"no systemd-creds", remoteexec.Result{RC: 127, Stderr: "systemd-creds: not found"}, "cannot determine"},
		{"rc 0 but not yes", remoteexec.Result{RC: 0, Stdout: "partial\n"}, "no usable TPM2"},
		{"ansi colours", remoteexec.Result{RC: 0, Stdout: "\x1b[0;32myes\x1b[0m\n"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := newFakeConn(map[string]remoteexec.Result{has: tc.has, enc: {Stdout: "CT\n"}})
			res, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{"secret": credsSecret, "require_tpm": true})
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantFail == "" {
				if res.Failed || res.Extra["value"] != "CT" {
					t.Fatalf("res = %+v", res)
				}
				if len(conn.Commands) != 2 || conn.Commands[0] != has || conn.Commands[1] != enc {
					t.Fatalf("commands = %q", conn.Commands)
				}
				return
			}
			if !res.Failed || !strings.Contains(res.Msg, tc.wantFail) {
				t.Fatalf("res = %+v, want failure containing %q", res, tc.wantFail)
			}
			for _, c := range conn.Commands {
				if strings.Contains(c, "encrypt") {
					t.Fatalf("encrypt ran after a failed TPM check: %q", conn.Commands)
				}
			}
			if _, ok := res.Extra["value"]; ok {
				t.Fatal("a value was returned without a TPM")
			}
		})
	}
}

func TestSystemdCredsRequireTPMWithKeys(t *testing.T) {
	for _, k := range []string{"host", "null", "auto-initrd"} {
		conn := newFakeConn(nil)
		if _, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{"secret": "x", "require_tpm": true, "with_key": k}); err == nil || len(conn.Commands) != 0 {
			t.Errorf("require_tpm with with_key=%s: err=%v cmds=%q", k, err, conn.Commands)
		}
	}
	// auto and tpm2 forms are checked, and auto is pinned to host+tpm2.
	for key, want := range map[string]string{"auto": "host+tpm2", "tpm2": "tpm2", "host+tpm2": "host+tpm2"} {
		conn := newFakeConn(map[string]remoteexec.Result{"systemd-creds has-tpm2": {Stdout: "yes\n"}})
		if _, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{"secret": "x", "require_tpm": true, "with_key": key}); err != nil {
			t.Fatal(err)
		}
		if len(conn.Commands) != 2 || conn.Commands[1] != "systemd-creds encrypt --with-key="+want+" - -" {
			t.Errorf("with_key=%s: commands %q", key, conn.Commands)
		}
	}
	// require_tpm false (the default) never probes.
	conn := newFakeConn(nil)
	if _, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{"secret": "x", "require_tpm": false}); err != nil {
		t.Fatal(err)
	}
	if len(conn.Commands) != 1 || strings.Contains(conn.Commands[0], "has-tpm2") {
		t.Fatalf("commands = %q", conn.Commands)
	}
}

// destFake scripts the probes dest makes. exists: whether dest exists;
// decrypt: the answer of decrypting it.
func destFake(exists bool, symlink bool, decrypt remoteexec.Result) (*fakeConn, string, string) {
	dest := "/etc/credstore/db.cred"
	probe := map[string]remoteexec.Result{
		"test -L " + dest: {RC: 1},
		"test -d " + dest: {RC: 1},
		"test -e " + dest: {RC: 1},
		"systemd-creds encrypt --with-key=tpm2 - -":                       {Stdout: "NEWCT\n"},
		"systemd-creds decrypt --name= " + dest + " -":                    decrypt,
		"systemd-creds decrypt --name=db --uid=self " + dest + " -":       decrypt,
		"systemd-creds decrypt --name= --tpm2-device=auto " + dest + " -": decrypt,
	}
	if exists {
		probe["test -e "+dest] = remoteexec.Result{RC: 0}
	}
	if symlink {
		probe["test -L "+dest] = remoteexec.Result{RC: 0}
	}
	return newFakeConn(probe), dest, systemdCredsAtomicWrite(dest, "")
}

func assertNoPlaintextLeak(t *testing.T, conn *fakeConn, res Result, allowStdin int) {
	t.Helper()
	for _, c := range conn.Commands {
		if strings.Contains(c, credsSecret) {
			t.Fatalf("command leaks the secret: %q", c)
		}
	}
	for i, s := range conn.Stdins {
		if i != allowStdin && strings.Contains(s, credsSecret) {
			t.Fatalf("stdin %d carries the plaintext: %q", i, s)
		}
	}
	if s := fmt.Sprintf("%+v", res); strings.Contains(s, credsSecret) {
		t.Fatalf("result leaks the secret: %s", s)
	}
}

func TestSystemdCredsDestNewFileIsWrittenAtomically(t *testing.T) {
	conn, dest, script := destFake(false, false, remoteexec.Result{})
	res, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{
		"secret": credsSecret, "with_key": "tpm2", "dest": dest,
	})
	if err != nil || res.Failed || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	want := []string{"test -L " + dest, "test -d " + dest, "test -e " + dest, "systemd-creds encrypt --with-key=tpm2 - -", script}
	if fmt.Sprint(conn.Commands) != fmt.Sprint(want) {
		t.Fatalf("commands = %q\nwant %q", conn.Commands, want)
	}
	if conn.Stdins[4] != "NEWCT\n" {
		t.Fatalf("write stdin = %q, want the ciphertext", conn.Stdins[4])
	}
	// mktemp in dest's directory, 0600, rename; temp removed on failure.
	for _, frag := range []string{"umask 077", "mktemp -- /etc/credstore/.db.cred.XXXXXX", "chmod 0600", "mv -f -- \"$t\" /etc/credstore/db.cred", "rm -f -- \"$t\"", "trap - EXIT"} {
		if !strings.Contains(script, frag) {
			t.Errorf("script lacks %q: %s", frag, script)
		}
	}
	if strings.Contains(script, "chown") {
		t.Error("chown without owner")
	}
	if res.Extra["dest"] != dest || res.Extra["value"] != "NEWCT" {
		t.Fatalf("extra = %v", res.Extra)
	}
	assertNoPlaintextLeak(t, conn, res, 3)
}

func TestSystemdCredsDestOwnerAndRoot(t *testing.T) {
	if s := systemdCredsAtomicWrite("/etc/credstore/db.cred", "app"); !strings.Contains(s, "chown -- app \"$t\" && mv -f") {
		t.Fatalf("owner not applied before the rename: %s", s)
	}
	if s := systemdCredsAtomicWrite("/x.cred", ""); !strings.Contains(s, "mktemp -- /.x.cred.XXXXXX") {
		t.Fatalf("root dir template: %s", s)
	}
	conn := newFakeConn(nil)
	if _, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{"secret": "x", "dest": "/etc/c", "owner": "a;b"}); err == nil || len(conn.Commands) != 0 {
		t.Fatalf("hostile owner: err=%v cmds=%q", err, conn.Commands)
	}
}

func TestSystemdCredsDestIdempotentWhenDecryptsEqual(t *testing.T) {
	conn, dest, _ := destFake(true, false, remoteexec.Result{Stdout: credsSecret})
	res, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{
		"secret": credsSecret, "with_key": "tpm2", "dest": dest,
	})
	if err != nil || res.Failed || res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	for _, c := range conn.Commands {
		if strings.Contains(c, "encrypt") || strings.Contains(c, "mktemp") {
			t.Fatalf("an unchanged dest was rewritten: %q", conn.Commands)
		}
	}
	if _, ok := res.Extra["value"]; ok {
		t.Fatal("value returned for an unchanged dest")
	}
	assertNoPlaintextLeak(t, conn, res, -1)
}

func TestSystemdCredsDestRewrittenWhenContentDiffers(t *testing.T) {
	for name, dec := range map[string]remoteexec.Result{
		"different": {Stdout: "old-secret"},
		"undecryptable (other key, corrupt, expired)": {RC: 1, Stderr: "Failed to decrypt"},
		"prefix only":   {Stdout: credsSecret[:5]},
		"extra newline": {Stdout: credsSecret + "\n"},
	} {
		t.Run(name, func(t *testing.T) {
			conn, dest, script := destFake(true, false, dec)
			res, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{
				"secret": credsSecret, "with_key": "tpm2", "dest": dest,
			})
			if err != nil || res.Failed || !res.Changed {
				t.Fatalf("res=%+v err=%v", res, err)
			}
			if last := conn.Commands[len(conn.Commands)-1]; last != script {
				t.Fatalf("last command = %q", last)
			}
			assertNoPlaintextLeak(t, conn, res, 4)
		})
	}
}

func TestSystemdCredsDestDecryptUsesNameUserDeviceAndSignature(t *testing.T) {
	dest := "/etc/credstore/db.cred"
	dec := "systemd-creds decrypt --name=db --uid=self --tpm2-device=auto --tpm2-signature=/s.json " + dest + " -"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -L " + dest: {RC: 1}, "test -d " + dest: {RC: 1}, "test -e " + dest: {RC: 0},
		dec: {Stdout: credsSecret},
	})
	res, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{
		"secret": credsSecret, "dest": dest, "name": "db", "user": "self",
		"tpm2_device": "auto", "tpm2_signature": "/s.json",
	})
	if err != nil || res.Failed || res.Changed {
		t.Fatalf("res=%+v err=%v cmds=%q", res, err, conn.Commands)
	}
	if conn.Commands[3] != dec {
		t.Fatalf("decrypt command = %q, want %q", conn.Commands[3], dec)
	}
}

func TestSystemdCredsDestRefusesSymlinkAndDirectory(t *testing.T) {
	conn, dest, _ := destFake(true, true, remoteexec.Result{Stdout: credsSecret})
	res, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{"secret": credsSecret, "with_key": "tpm2", "dest": dest})
	if err != nil || !res.Failed || !strings.Contains(res.Msg, "symlink") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	for _, c := range conn.Commands {
		if strings.Contains(c, "encrypt") || strings.Contains(c, "mktemp") || strings.Contains(c, "decrypt") {
			t.Fatalf("ran %q against a symlink", c)
		}
	}
	conn2 := newFakeConn(map[string]remoteexec.Result{"test -L " + dest: {RC: 1}, "test -d " + dest: {RC: 0}})
	res, err = moduleSystemdCredsEncrypt(context.Background(), conn2, map[string]any{"secret": credsSecret, "dest": dest})
	if err != nil || !res.Failed || !strings.Contains(res.Msg, "directory") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestSystemdCredsDestArgumentConflicts(t *testing.T) {
	for _, a := range []map[string]any{
		{"secret": "x", "dest": "/etc/c", "pretty": true},
		{"secret": "x", "owner": "app"},
		{"secret": "x", "tpm2_signature": "/s.json"},
		{"secret": "x", "dest": "/etc/dir/"},
	} {
		conn := newFakeConn(nil)
		if _, err := moduleSystemdCredsEncrypt(context.Background(), conn, a); err == nil || len(conn.Commands) != 0 {
			t.Errorf("%v: err=%v cmds=%q", a, err, conn.Commands)
		}
	}
}

func TestSystemdCredsDestCheckModeWritesNothing(t *testing.T) {
	conn, dest, _ := destFake(true, false, remoteexec.Result{Stdout: "old"})
	res, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{
		"secret": credsSecret, "with_key": "tpm2", "dest": dest, CheckModeKey: true,
	})
	if err != nil || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	for _, c := range conn.Commands {
		if strings.Contains(c, "mktemp") || strings.Contains(c, "encrypt") {
			t.Fatalf("check mode ran %q", c)
		}
	}
}

func TestSystemdCredsFailureMessagesMaskTheSecret(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemd-creds encrypt - -": {RC: 1, Stderr: "bad input " + credsSecret},
	})
	res, _ := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{"secret": credsSecret})
	if !res.Failed || strings.Contains(res.Msg, credsSecret) || !strings.Contains(res.Msg, "********") {
		t.Fatalf("msg = %q", res.Msg)
	}
	dc := newFakeConn(map[string]remoteexec.Result{
		"systemd-creds decrypt - -": {RC: 1, Stderr: "cannot parse " + credsSecret},
	})
	res, _ = moduleSystemdCredsDecrypt(context.Background(), dc, map[string]any{"secret": credsSecret})
	if !res.Failed || strings.Contains(res.Msg, credsSecret) {
		t.Fatalf("msg = %q", res.Msg)
	}
}

func TestSystemdCredsWithoutDestIsStillNeverChanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{"systemd-creds encrypt --with-key=null - -": {Stdout: "CT\n"}})
	res, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{"secret": "x", "with_key": "null"})
	if err != nil || res.Changed || res.Failed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// TestSystemdCredsRealHostKey drives the real systemd-creds with
// --with-key=host: CI has no TPM, so the TPM2 paths are covered only by the
// fake above and this test proves everything else (the real command line,
// the real atomic write, the real decrypt-and-compare idempotence). Needs
// root, because the host key lives in /var/lib/systemd and is created by
// `systemd-creds setup`. MODULES_REQUIRE_SYSTEMD_CREDS makes a skip a failure.
func TestSystemdCredsRealHostKey(t *testing.T) {
	require := os.Getenv("MODULES_REQUIRE_SYSTEMD_CREDS") != ""
	skip := func(why string) {
		if require {
			t.Fatalf("MODULES_REQUIRE_SYSTEMD_CREDS is set but %s", why)
		}
		t.Skip(why)
	}
	if _, err := exec.LookPath("systemd-creds"); err != nil {
		skip("systemd-creds is not installed")
	}
	if os.Geteuid() != 0 {
		skip("not root: the host key is in /var/lib/systemd")
	}
	ctx := context.Background()
	conn := local()
	dest := filepath.Join(t.TempDir(), "db.cred")
	run1 := func(secret string) Result {
		t.Helper()
		res, err := moduleSystemdCredsEncrypt(ctx, conn, map[string]any{"secret": secret, "with_key": "host", "name": "db", "dest": dest})
		if err != nil || res.Failed {
			t.Fatalf("encrypt: res=%+v err=%v", res, err)
		}
		return res
	}
	if r := run1("hunter2"); !r.Changed {
		t.Fatalf("first write not changed: %+v", r)
	}
	st, err := os.Stat(dest)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("stat = %v, %v; want mode 0600", st, err)
	}
	raw, _ := os.ReadFile(dest)
	if strings.Contains(string(raw), "hunter2") {
		t.Fatal("plaintext in the file")
	}
	if r := run1("hunter2"); r.Changed {
		t.Fatalf("same secret rewrote dest: %+v", r)
	}
	if r := run1("hunter3"); !r.Changed {
		t.Fatalf("new secret not written: %+v", r)
	}
	dec, err := moduleSystemdCredsDecrypt(ctx, conn, map[string]any{"secret": strings.TrimSpace(string(mustRead(t, dest))), "name": "db"})
	if err != nil || dec.Failed || dec.Extra["value"] != "hunter3" {
		t.Fatalf("round trip: %+v err=%v", dec, err)
	}
	link := filepath.Join(filepath.Dir(dest), "link.cred")
	if err := os.Symlink(dest, link); err != nil {
		t.Fatal(err)
	}
	res, err := moduleSystemdCredsEncrypt(ctx, conn, map[string]any{"secret": "x", "with_key": "host", "dest": link})
	if err != nil || !res.Failed {
		t.Fatalf("symlink dest: %+v err=%v", res, err)
	}
	if fi, _ := os.Lstat(link); fi == nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced")
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// errAt fails the nth Exec (1-based) with a transport error, as a dropped
// connection would.
type errAt struct {
	*fakeConn
	n, calls int
}

func (e *errAt) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	e.calls++
	if e.calls == e.n {
		return remoteexec.Result{}, errors.New("transport down")
	}
	return e.fakeConn.Exec(ctx, cmd, stdin)
}

func TestSystemdCredsTransportErrorsAreReturnedAtEveryStep(t *testing.T) {
	dest := "/etc/credstore/db.cred"
	// has-tpm2, test -L, test -d, test -e, decrypt, encrypt, write.
	for n := 1; n <= 7; n++ {
		inner := newFakeConn(map[string]remoteexec.Result{
			"systemd-creds has-tpm2":                         {Stdout: "yes\n"},
			"test -e " + dest:                                {RC: 0},
			"systemd-creds decrypt --name= " + dest + " -":   {Stdout: "other"},
			"test -L " + dest:                                {RC: 1},
			"test -d " + dest:                                {RC: 1},
			"systemd-creds encrypt --with-key=host+tpm2 - -": {Stdout: "CT\n"},
		})
		c := &errAt{fakeConn: inner, n: n}
		res, err := moduleSystemdCredsEncrypt(context.Background(), c, map[string]any{"secret": credsSecret, "require_tpm": true, "dest": dest})
		if err == nil {
			t.Errorf("exec %d failing: err = nil, res = %+v", n, res)
		}
	}
	for n := 1; n <= 1; n++ {
		c := &errAt{fakeConn: newFakeConn(nil), n: 1}
		if _, err := moduleSystemdCredsDecrypt(context.Background(), c, map[string]any{"secret": "x"}); err == nil {
			t.Error("decrypt: transport error swallowed")
		}
		c = &errAt{fakeConn: newFakeConn(nil), n: 1}
		if _, err := moduleSystemdCredsEncrypt(context.Background(), c, map[string]any{"secret": "x"}); err == nil {
			t.Error("encrypt: transport error swallowed")
		}
	}
}

func TestSystemdCredsWriteFailureIsReported(t *testing.T) {
	conn, dest, script := destFake(false, false, remoteexec.Result{})
	conn.on[script] = remoteexec.Result{RC: 1, Stderr: "No space left on device"}
	res, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{"secret": credsSecret, "with_key": "tpm2", "dest": dest})
	if err != nil || !res.Failed || res.Changed || !strings.Contains(res.Msg, "No space left") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// A failed decrypt is "different" even if the fake still prints something
// equal to the secret on stdout: the exit status decides, not the bytes.
func TestSystemdCredsDestDecryptFailureWinsOverStdout(t *testing.T) {
	conn, dest, _ := destFake(true, false, remoteexec.Result{RC: 1, Stdout: credsSecret})
	res, err := moduleSystemdCredsEncrypt(context.Background(), conn, map[string]any{"secret": credsSecret, "with_key": "tpm2", "dest": dest})
	if err != nil || res.Failed || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestSystemdCredsScrub(t *testing.T) {
	if got := systemdCredsScrub("abc", ""); got != "abc" {
		t.Fatalf("empty secret rewrote the message: %q", got)
	}
	if got := systemdCredsScrub("a s b s", "s"); got != "a ******** b ********" {
		t.Fatalf("got %q", got)
	}
}
