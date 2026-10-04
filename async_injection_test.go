package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// ⛔ SECURITY REGRESSION TEST. `jid` arrives from a playbook argument
// (async_status: {jid: ...}) and was interpolated into shell commands
// UNQUOTED, including into `rm -rf $HOME/.ansible_async/<jid>`.
//
// Demonstrated before the fix: a jid of `x; touch <path> ;` created
// that file. The payload here is deliberately harmless -- it only
// touches a marker inside the test's own TempDir -- and the test fails
// if the marker appears, which is what injection looks like.
//
// Both AsyncCleanup and AsyncCheck are covered, because they
// interpolate jid in different commands and fixing one would leave the
// other open.
func TestAsyncJIDCannotInject(t *testing.T) {
	for _, call := range []struct {
		name string
		run  func(jid string)
	}{
		{"AsyncCleanup", func(jid string) { _ = AsyncCleanup(context.Background(), remoteexec.NewLocal(), jid) }},
		{"AsyncCheck", func(jid string) {
			_, _, _, _, _, _ = AsyncCheck(context.Background(), remoteexec.NewLocal(), jid)
		}},
	} {
		t.Run(call.name, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "INJECTED")
			call.run("x; touch " + marker + " ; echo")
			if _, err := os.Stat(marker); err == nil {
				t.Errorf("%s let a jid reach the shell as code: %s was created", call.name, marker)
			}
		})
	}
}

// The shape check must not change what a caller SEES. Real accepts a
// malformed jid and reports its ordinary not-found result -- measured:
// `async_status: {jid: nope}` gives msg "could not find job" with
// started/finished true, not an error.
//
// A first version of this rejected the shape outright and broke that
// parity; TestModuleAsyncStatusNotFound caught it. Security and parity
// both hold here only because the malformed case maps to real's own
// answer rather than to an error.
func TestAsyncJIDShapeMapsToNotFoundNotAnError(t *testing.T) {
	if err := AsyncCleanup(context.Background(), remoteexec.NewLocal(), "not-a-jid"); err != nil {
		t.Errorf("a malformed jid errored; real reports not-found: %v", err)
	}
	found, _, _, _, _, err := AsyncCheck(context.Background(), remoteexec.NewLocal(), "not-a-jid")
	if err != nil {
		t.Errorf("AsyncCheck errored on a malformed jid: %v", err)
	}
	if found {
		t.Error("a malformed jid was reported as a found job")
	}
	// the shape this package itself generates must pass
	if !validAsyncJID("1791135913379655000.123456") {
		t.Error("a real jid was rejected")
	}
}
