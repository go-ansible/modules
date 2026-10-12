package modules

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// uid/group/comment/non_unique for the user module, against fakeConn
// (see fakeconn_test.go): useradd and usermod need root, so these
// assert the commands issued and the changed flag, not a real passwd
// database. The expected command lines follow real ansible-core
// stable-2.21's create_user_useradd / modify_user_usermod as read from
// the source, not as measured against a run of it.

const slurmPasswd = "slurm:x:64030:64030:SLURM workload manager:/var/lib/slurm:/bin/false\n"

func assertNoCommandPrefix(t *testing.T, conn *fakeConn, prefix string) {
	t.Helper()
	for _, c := range conn.Commands {
		if strings.HasPrefix(c, prefix) {
			t.Fatalf("unexpected %q in %v", prefix, conn.Commands)
		}
	}
}

// The reported failure: the group was created first, so useradd
// without -g tried to create a same-named group and refused. With
// group given, -g is passed and uid is no longer dropped.
func TestModuleUserCreateUIDAndExistingGroup(t *testing.T) {
	for _, uid := range []any{64030, "64030", float64(64030)} {
		conn := newFakeConn(map[string]remoteexec.Result{
			"getent passwd slurm": {RC: 2},
			"getent group slurm":  {RC: 0, Stdout: "slurm:x:64030:\n"},
		})
		res, err := moduleUser(context.Background(), conn, map[string]any{
			"name": "slurm", "uid": uid, "group": "slurm", "system": true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Changed || res.Failed {
			t.Fatalf("uid %T: want changed, got %+v", uid, res)
		}
		want := "useradd -m -r -u 64030 -g slurm slurm"
		if got := lastCommand(conn); got != want {
			t.Fatalf("uid %T: cmd = %q, want %q", uid, got, want)
		}
	}
}

// group omitted, but a group named like the user exists: real passes
// -N so useradd does not try (and fail) to create it.
func TestModuleUserCreateSameNamedGroupPassesN(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd slurm": {RC: 2},
		"getent group slurm":  {RC: 0, Stdout: "slurm:x:64030:\n"},
	})
	res, err := moduleUser(context.Background(), conn, map[string]any{"name": "slurm", "uid": 64030})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if got, want := lastCommand(conn), "useradd -m -u 64030 -N slurm"; got != want {
		t.Fatalf("cmd = %q, want %q", got, want)
	}
}

func TestModuleUserCreateMissingGroupFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd bob": {RC: 2},
		"getent group nope": {RC: 2},
	})
	res, err := moduleUser(context.Background(), conn, map[string]any{"name": "bob", "group": "nope"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || res.Msg != "Group nope does not exist" {
		t.Fatalf("want real's failure message, got %+v", res)
	}
	assertNoCommandPrefix(t, conn, "useradd")
}

func TestModuleUserCreateNonUniqueAndComment(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd toor": {RC: 2},
		"getent group root":  {RC: 0, Stdout: "root:x:0:\n"},
	})
	res, err := moduleUser(context.Background(), conn, map[string]any{
		"name": "toor", "uid": 0, "non_unique": true, "group": "root",
		"comment": "Toor Account", "create_home": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	want := "useradd -M -u 0 -o -g root -c 'Toor Account' toor"
	if got := lastCommand(conn); got != want {
		t.Fatalf("cmd = %q, want %q", got, want)
	}
}

// non_unique without uid adds nothing: real appends -o only next to -u.
func TestModuleUserCreateNonUniqueWithoutUID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd bob": {RC: 2},
		"getent group bob":  {RC: 2},
	})
	if _, err := moduleUser(context.Background(), conn, map[string]any{"name": "bob", "non_unique": true}); err != nil {
		t.Fatal(err)
	}
	if got, want := lastCommand(conn), "useradd -m bob"; got != want {
		t.Fatalf("cmd = %q, want %q", got, want)
	}
}

// Every compared field already matches: no usermod, not changed —
// the case the old "always changed" behaviour got wrong for shell and
// home, and could not express at all for uid/group/comment.
func TestModuleUserExistingAllMatchUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd slurm": {RC: 0, Stdout: slurmPasswd},
		"getent group slurm":  {RC: 0, Stdout: "slurm:x:64030:\n"},
	})
	res, err := moduleUser(context.Background(), conn, map[string]any{
		"name": "slurm", "uid": 64030, "non_unique": true, "group": "slurm",
		"comment": "SLURM workload manager", "home": "/var/lib/slurm", "shell": "/bin/false",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, commands = %v", conn.Commands)
	}
	assertNoCommandPrefix(t, conn, "usermod")
}

// group given as a numeric GID resolves through the same getent call.
func TestModuleUserExistingNumericGroupMatches(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd slurm": {RC: 0, Stdout: slurmPasswd},
		"getent group 64030":  {RC: 0, Stdout: "slurm:x:64030:\n"},
	})
	res, err := moduleUser(context.Background(), conn, map[string]any{"name": "slurm", "group": "64030"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, commands = %v", conn.Commands)
	}
}

func TestModuleUserExistingDiffersModifiesOnlyDifferences(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd slurm": {RC: 0, Stdout: slurmPasswd},
		"getent group hpc":    {RC: 0, Stdout: "hpc:x:5000:slurm\n"},
	})
	res, err := moduleUser(context.Background(), conn, map[string]any{
		"name": "slurm", "uid": 64031, "non_unique": true, "group": "hpc",
		"comment": "Slurm", "shell": "/bin/false", "home": "/var/lib/slurm",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	// home and shell match and stay out; group goes in as the GID.
	want := "usermod -u 64031 -o -g 5000 -c Slurm slurm"
	if got := lastCommand(conn); got != want {
		t.Fatalf("cmd = %q, want %q", got, want)
	}
}

// comment: "" is a request to clear the GECOS field, not "unset".
func TestModuleUserExistingEmptyCommentClears(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd slurm": {RC: 0, Stdout: slurmPasswd},
	})
	if _, err := moduleUser(context.Background(), conn, map[string]any{"name": "slurm", "comment": ""}); err != nil {
		t.Fatal(err)
	}
	if got, want := lastCommand(conn), "usermod -c '' slurm"; got != want {
		t.Fatalf("cmd = %q, want %q", got, want)
	}
}

// groups is still not compared, so it is still issued — the remaining
// documented limitation.
func TestModuleUserExistingGroupsStillAlwaysIssued(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd slurm": {RC: 0, Stdout: slurmPasswd},
	})
	res, err := moduleUser(context.Background(), conn, map[string]any{
		"name": "slurm", "groups": []any{"munge"}, "shell": "/bin/false",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if got, want := lastCommand(conn), "usermod -G munge slurm"; got != want {
		t.Fatalf("cmd = %q, want %q", got, want)
	}
}

func TestModuleUserExistingMissingGroupFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd slurm": {RC: 0, Stdout: slurmPasswd},
		"getent group nope":   {RC: 2},
	})
	res, err := moduleUser(context.Background(), conn, map[string]any{"name": "slurm", "group": "nope"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || res.Msg != "Group nope does not exist" {
		t.Fatalf("want real's failure message, got %+v", res)
	}
	assertNoCommandPrefix(t, conn, "usermod")
}

func TestModuleUserExistingUnparseableEntryErrors(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd slurm": {RC: 0, Stdout: "garbage\n"},
	})
	if _, err := moduleUser(context.Background(), conn, map[string]any{"name": "slurm", "uid": 1}); err == nil {
		t.Fatal("want an error rather than a guessed comparison")
	}
	assertNoCommandPrefix(t, conn, "usermod")
}

func TestModuleUserUnparseableGroupEntryErrors(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd slurm": {RC: 0, Stdout: slurmPasswd},
		"getent group slurm":  {RC: 0, Stdout: "slurm\n"},
	})
	if _, err := moduleUser(context.Background(), conn, map[string]any{"name": "slurm", "group": "slurm"}); err == nil {
		t.Fatal("want an error")
	}
}

func TestModuleUserBadUID(t *testing.T) {
	for _, uid := range []any{"abc", 1.5, true} {
		conn := newFakeConn(map[string]remoteexec.Result{"getent passwd bob": {RC: 2}})
		if _, err := moduleUser(context.Background(), conn, map[string]any{"name": "bob", "uid": uid}); err == nil {
			t.Fatalf("uid %v: want an argument error", uid)
		}
	}
}

func TestModuleUserGroupTransportErrors(t *testing.T) {
	// errConn (TestTransportErrorsPropagate) fails every call, so the
	// first getent already errors there; the group probes' own error
	// paths need a conn that answers getent passwd and fails afterwards.
	// failAfterConn (failafter_test.go) answers with an EMPTY stdout,
	// which is not a passwd entry, hence the scripted variant below.
	for _, args := range []map[string]any{
		{"name": "bob"},                  // new user, same-named group probe
		{"name": "bob", "group": "devs"}, // new user, -g probe
	} {
		conn := &scriptedFailAfterConn{fakeConn: newFakeConn(map[string]remoteexec.Result{"getent passwd bob": {RC: 2}}), ok: 1}
		if _, err := moduleUser(context.Background(), conn, args); err == nil {
			t.Fatalf("%v: want a transport error", args)
		}
	}
	conn := &scriptedFailAfterConn{fakeConn: newFakeConn(map[string]remoteexec.Result{"getent passwd bob": {RC: 0, Stdout: "bob:x:1:1::/h:/s\n"}}), ok: 1}
	if _, err := moduleUser(context.Background(), conn, map[string]any{"name": "bob", "group": "devs"}); err == nil {
		t.Fatal("existing user: want a transport error")
	}
}

// scriptedFailAfterConn answers its first ok Exec calls from the
// wrapped fakeConn's script and fails every one after that.
type scriptedFailAfterConn struct {
	*fakeConn
	ok int
}

func (f *scriptedFailAfterConn) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	if f.ok <= 0 {
		return remoteexec.Result{}, errors.New("transport down")
	}
	f.ok--
	return f.fakeConn.Exec(ctx, cmd, stdin)
}
