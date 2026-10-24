package modules

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// owner/group and force: false for copy and template.
//
// Changing a file's owner needs root, so the chown paths run against
// fakeConn (fakeconn_test.go) and assert the commands issued. The
// no-op paths -- asking for the owner and group the file already has --
// and force: false run against the real local connection, where the
// result on disk is what is asserted.

// statCmd is the exact probe statPath sends, so a fakeConn can script
// its answer.
func statCmd(path string) string {
	q := shellQuote(path)
	return fmt.Sprintf(
		"stat -c '%%s|%%a|%%F|%%u|%%g|%%U|%%G' %s 2>/dev/null || stat -f '%%z|%%Lp|%%HT|%%u|%%g|%%Su|%%Sg' %s 2>/dev/null",
		q, q,
	)
}

func indexOf(cmds []string, want string) int {
	for i, c := range cmds {
		if c == want {
			return i
		}
	}
	return -1
}

func TestChownSpec(t *testing.T) {
	root := &fileInfo{uid: 0, gid: 0, owner: "root", group: "root"}
	unknown := &fileInfo{} // a stat that reported no ownership
	for _, tc := range []struct {
		name         string
		fi           *fileInfo
		owner, group string
		want         string
	}{
		{"nothing asked", root, "", "", ""},
		{"same by name", root, "root", "root", ""},
		{"same by id", root, "0", "0", ""},
		{"owner differs", root, "munge", "root", "munge"},
		{"group differs", root, "root", "munge", ":munge"},
		{"both differ", root, "munge", "munge", "munge:munge"},
		{"missing file", nil, "munge", "", "munge"},
		{"ownership unknown", unknown, "root", "root", "root:root"},
	} {
		if got := chownSpec(tc.fi, tc.owner, tc.group); got != tc.want {
			t.Errorf("%s: chownSpec = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The reported failure: a root-run copy with owner/group left the file
// root:root. Now chown runs, before chmod as in real.
func TestCopyNewFileAppliesOwnerGroupBeforeMode(t *testing.T) {
	dest := "/etc/munge/munge.key"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + dest: {RC: 1},
		statCmd(dest):     {RC: 0, Stdout: "5|644|regular file|0|0|root|root\n"},
	})
	res, err := moduleCopy(context.Background(), conn, map[string]any{
		"content": "key\n", "dest": dest, "owner": "munge", "group": "munge", "mode": "0400",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	chown := indexOf(conn.Commands, "chown munge:munge "+dest)
	chmod := indexOf(conn.Commands, "chmod 0400 "+dest)
	if chown < 0 || chmod < 0 || chown > chmod {
		t.Fatalf("want chown then chmod, commands = %v", conn.Commands)
	}
}

// Content and ownership already right: no chown, not changed.
// content "" because fakeConn's Fetch writes nothing, so an existing
// dest reads back empty.
func TestCopyExistingOwnershipMatchesUnchanged(t *testing.T) {
	dest := "/srv/home/ubuntu/.ssh/authorized_keys"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + dest: {RC: 0},
		statCmd(dest):     {RC: 0, Stdout: "0|600|regular file|1000|1000|ubuntu|ubuntu\n"},
	})
	res, err := moduleCopy(context.Background(), conn, map[string]any{
		"content": "", "dest": dest, "owner": "ubuntu", "group": "1000", "mode": "0600",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, commands = %v", conn.Commands)
	}
	for _, c := range conn.Commands {
		if c == "chown ubuntu:1000 "+dest || c == "chmod 0600 "+dest {
			t.Fatalf("unexpected %q", c)
		}
	}
}

// Only the differing half is passed to chown.
func TestCopyExistingOnlyGroupDiffers(t *testing.T) {
	dest := "/etc/app.conf"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + dest: {RC: 0},
		statCmd(dest):     {RC: 0, Stdout: "0|644|regular file|0|0|root|root\n"},
	})
	res, err := moduleCopy(context.Background(), conn, map[string]any{
		"content": "", "dest": dest, "owner": "root", "group": "app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if indexOf(conn.Commands, "chown :app "+dest) < 0 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestCopyOwnerCheckModeReportsWithoutChown(t *testing.T) {
	dest := "/etc/app.conf"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + dest: {RC: 0},
		statCmd(dest):     {RC: 0, Stdout: "0|644|regular file|0|0|root|root\n"},
	})
	res, err := moduleCopy(context.Background(), conn, map[string]any{
		"content": "", "dest": dest, "owner": "app", CheckModeKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed in check mode")
	}
	if indexOf(conn.Commands, "chown app "+dest) >= 0 {
		t.Fatalf("check mode ran chown: %v", conn.Commands)
	}
}

func TestCopyChownFailurePropagates(t *testing.T) {
	dest := "/etc/app.conf"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + dest:      {RC: 0},
		statCmd(dest):          {RC: 0, Stdout: "0|644|regular file|0|0|root|root\n"},
		"chown nosuch " + dest: {RC: 1, Stderr: "chown: invalid user: 'nosuch'"},
	})
	if _, err := moduleCopy(context.Background(), conn, map[string]any{
		"content": "", "dest": dest, "owner": "nosuch",
	}); err == nil {
		t.Fatal("want the chown failure")
	}
}

func TestTemplateAppliesOwnerGroup(t *testing.T) {
	src := filepath.Join(t.TempDir(), "munge.conf.j2")
	if err := os.WriteFile(src, []byte("x={{ v }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := "/etc/munge/munge.conf"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + dest: {RC: 1},
		statCmd(dest):     {RC: 0, Stdout: "4|644|regular file|0|0|root|root\n"},
	})
	res, err := moduleTemplate(context.Background(), conn, map[string]any{
		"src": src, "dest": dest, "owner": "munge", "group": "munge",
		"_vars": map[string]any{"v": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if indexOf(conn.Commands, "chown munge:munge "+dest) < 0 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

// fileOwnership is the uid/gid a file really has, and the owner's name.
func fileOwnership(t *testing.T, path string) (uid, gid, ownerName string) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("no syscall.Stat_t on this platform")
	}
	uid = strconv.FormatUint(uint64(st.Uid), 10)
	gid = strconv.FormatUint(uint64(st.Gid), 10)
	u, err := user.LookupId(uid)
	if err != nil {
		t.Skipf("cannot name uid %s: %v", uid, err)
	}
	return uid, gid, u.Username
}

// Against the real local connection: asking for the owner and group a
// file already has is not a change, by name or by ID.
func TestCopyLocalOwnerAlreadyRightIsUnchanged(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "f.txt")
	ctx := context.Background()
	conn := local()
	if _, err := moduleCopy(ctx, conn, map[string]any{"content": "x\n", "dest": dest}); err != nil {
		t.Fatal(err)
	}
	uid, gid, name := fileOwnership(t, dest)
	for _, owner := range []string{uid, name} {
		res, err := moduleCopy(ctx, conn, map[string]any{
			"content": "x\n", "dest": dest, "owner": owner, "group": gid,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Changed {
			t.Errorf("owner %q group %q: want unchanged", owner, gid)
		}
	}
}

func TestCopyForceFalseLeavesExistingFileAlone(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(dest, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := moduleCopy(context.Background(), local(), map[string]any{
		"content": "new\n", "dest": dest, "force": false, "mode": "0600",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	data, _ := os.ReadFile(dest)
	fi, _ := os.Stat(dest)
	if string(data) != "old\n" || fi.Mode().Perm() != 0o644 {
		t.Fatalf("dest touched: %q %04o (real applies neither content nor mode)", data, fi.Mode().Perm())
	}
	if res.Extra["dest"] != dest {
		t.Errorf("dest = %v", res.Extra["dest"])
	}
	if v, ok := res.Extra["src"]; !ok || v != nil {
		t.Errorf("src = %v (present %v), want real's None for content:", v, ok)
	}
}

func TestCopyForceFalseStillCreatesMissingFile(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "f.txt")
	res, err := moduleCopy(context.Background(), local(), map[string]any{
		"content": "new\n", "dest": dest, "force": "no",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if data, _ := os.ReadFile(dest); string(data) != "new\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestTemplateForceFalseLeavesExistingFileAlone(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "t.j2")
	dest := filepath.Join(dir, "out")
	if err := os.WriteFile(src, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := moduleTemplate(context.Background(), local(), map[string]any{
		"src": src, "dest": dest, "force": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if data, _ := os.ReadFile(dest); string(data) != "old\n" {
		t.Fatalf("dest overwritten: %q", data)
	}
}

func TestCopyForceFalseTransportError(t *testing.T) {
	if _, err := moduleCopy(context.Background(), errConn{}, map[string]any{
		"content": "x", "dest": "/x", "force": false,
	}); err == nil {
		t.Fatal("want a transport error")
	}
}
