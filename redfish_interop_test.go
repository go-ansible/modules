package modules

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/go-redfish/redfish/mockbmc"
)

// The oracle for these tests is REAL ansible-core with the real
// community.general collection, run against the same mock BMC as the
// in-process backend. A lane that cannot find them SKIPS, unless
// ANSIBLE_REQUIRE is set, which turns the skip into a failure (the same
// convention as go-ansible/playbook's interop lanes).
//
// The collection under test is community.general at the commit PARITY.md of
// go-redfish names (33e521d6); ANSIBLE_COLLECTIONS_PATH points at a checkout.

// interopBMC is one mock BMC served over TLS, with its certificate written
// to a PEM file that real ansible trusts through SSL_CERT_FILE.
type interopBMC struct {
	bmc     *mockbmc.BMC
	baseuri string
	caFile  string
	srv     *httptest.Server
}

func startInteropBMC(t *testing.T, o mockbmc.Options) *interopBMC {
	t.Helper()
	b, err := mockbmc.New(o)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewTLSServer(b.Handler())
	t.Cleanup(srv.Close)
	cert := srv.Certificate()
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(ca, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	_ = x509.NewCertPool
	return &interopBMC{bmc: b, baseuri: strings.TrimPrefix(srv.URL, "https://"), caFile: ca, srv: srv}
}

func requireAnsible(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("ansible")
	if err != nil {
		if os.Getenv("ANSIBLE_REQUIRE") != "" {
			t.Fatalf("ANSIBLE_REQUIRE is set but ansible is not installed: %v", err)
		}
		t.Skip("ansible is not installed")
	}
	return p
}

// realAnsible runs community.general.<module> on localhost against the mock,
// as real ansible does, and returns the module's result.
func realAnsible(t *testing.T, ansible, module string, bmc *interopBMC, args map[string]any) map[string]any {
	t.Helper()
	a := map[string]any{"baseuri": bmc.baseuri, "username": "admin", "password": "password"}
	for k, v := range args {
		a[k] = v
	}
	js, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(ansible, "localhost", "-m", "community.general."+module, "-a", string(js))
	cmd.Env = append(os.Environ(), "SSL_CERT_FILE="+bmc.caFile, "ANSIBLE_LOCALHOST_WARNING=False",
		"ANSIBLE_INVENTORY_UNPARSED_WARNING=False", "ANSIBLE_DEPRECATION_WARNINGS=False", "ANSIBLE_NOCOLOR=1")
	var out bytes.Buffer
	cmd.Stdout = &out
	_ = cmd.Run() // a failed module exits non-zero; the JSON says why
	s := out.String()
	i := strings.Index(s, "=> {")
	if i < 0 {
		t.Fatalf("real ansible gave no result for %s %s:\n%s", module, js, s)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(s[i+3:]), &res); err != nil {
		t.Fatalf("real ansible result for %s is not JSON: %v\n%s", module, err, s)
	}
	// The ad-hoc printer says FAILED! in its header and leaves "failed" out of
	// the JSON; the engine's result carries it.
	if strings.Contains(s[:i], "FAILED!") {
		res["failed"] = true
	}
	return res
}

// goResult renders a Result the way the engine does for the keys that matter
// here: changed, failed, msg and the module's own extra keys.
func goResult(r Result) map[string]any {
	m := map[string]any{"changed": r.Changed}
	if r.Failed {
		m["failed"] = true
	}
	if r.Msg != "" || !r.NoMsg {
		m["msg"] = r.Msg
	}
	for k, v := range r.Extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

// volatile keys: values that differ between two runs of the same command on
// two BMCs (timestamps, task numbers) are not part of the comparison.
func interopDiff(want, got any, path string, out *[]string) {
	if reflect.DeepEqual(want, got) {
		return
	}
	wm, wok := want.(map[string]any)
	gm, gok := got.(map[string]any)
	if wok && gok {
		keys := map[string]bool{}
		for k := range wm {
			keys[k] = true
		}
		for k := range gm {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			wv, win := wm[k]
			gv, gin := gm[k]
			switch {
			case !win:
				*out = append(*out, fmt.Sprintf("%s.%s: only in go-redfish: %s", path, k, short(gv)))
			case !gin:
				*out = append(*out, fmt.Sprintf("%s.%s: only in real: %s", path, k, short(wv)))
			default:
				interopDiff(wv, gv, path+"."+k, out)
			}
		}
		return
	}
	wl, wlok := want.([]any)
	gl, glok := got.([]any)
	if wlok && glok && len(wl) == len(gl) {
		for i := range wl {
			interopDiff(wl[i], gl[i], fmt.Sprintf("%s[%d]", path, i), out)
		}
		return
	}
	*out = append(*out, fmt.Sprintf("%s: real=%s go-redfish=%s", path, short(want), short(got)))
}

func short(v any) string {
	b, _ := json.Marshal(v)
	if len(b) > 160 {
		return string(b[:160]) + "..."
	}
	return string(b)
}

// interopCase is one task run through real ansible and through the go-redfish
// backend, each on its own fresh mock (a state-changing command must not see
// the other's change).
type interopCase struct {
	module string
	args   map[string]any
	tree   string
	opts   func(*mockbmc.Options)
	// knownDiff lists the dotted paths under which the two backends are known to
	// differ and the reason; the test fails if such a path stops differing
	// (stale note) or any other path differs (new gap).
	knownDiff map[string]string
}

func runGoBackend(t *testing.T, bmc *interopBMC, module string, args map[string]any) map[string]any {
	t.Helper()
	a := map[string]any{"baseuri": bmc.baseuri, "username": "admin", "password": "password",
		"ca_path": bmc.caFile, RedfishBackendKey: RedfishBackendGoRedfish}
	for k, v := range args {
		a[k] = v
	}
	var res Result
	var err error
	switch module {
	case "redfish_info":
		res, err = moduleRedfishInfo(context.Background(), nil, a)
	case "redfish_command":
		res, err = moduleRedfishCommand(context.Background(), nil, a)
	case "redfish_config":
		res, err = moduleRedfishConfig(context.Background(), nil, a)
	default:
		t.Fatalf("unknown module %s", module)
	}
	if err != nil {
		t.Fatalf("%s: %v", module, err)
	}
	return goResult(res)
}

func runInteropCase(t *testing.T, ansible string, c interopCase) {
	t.Helper()
	o := mockbmc.Options{Tree: c.tree}
	if c.opts != nil {
		c.opts(&o)
	}
	real := startInteropBMC(t, o)
	gob := startInteropBMC(t, o)
	want := realAnsible(t, ansible, c.module, real, c.args)
	got := runGoBackend(t, gob, c.module, c.args)
	// the two BMCs differ in address only: normalise it.
	norm := func(v map[string]any, bmc *interopBMC) map[string]any {
		b, _ := json.Marshal(v)
		b = bytes.ReplaceAll(b, []byte(bmc.baseuri), []byte("BMC"))
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		return out
	}
	want, got = norm(want, real), norm(got, gob)
	delete(want, "invocation")
	delete(want, "_ansible_no_log")
	var diffs []string
	interopDiff(want, got, "", &diffs)
	if os.Getenv("REDFISH_INTEROP_DUMP") != "" {
		t.Logf("real: %s", short2(want))
		t.Logf("go:   %s", short2(got))
	}
	for _, d := range diffs {
		known := false
		for p := range c.knownDiff {
			if strings.HasPrefix(d, p+":") || strings.HasPrefix(d, p+".") || strings.HasPrefix(d, p+"[") {
				known = true
			}
		}
		if !known {
			t.Errorf("%s differs: %s", c.module, d)
		}
	}
	for p, why := range c.knownDiff {
		found := false
		for _, d := range diffs {
			if strings.HasPrefix(d, p+":") || strings.HasPrefix(d, p+".") || strings.HasPrefix(d, p+"[") {
				found = true
			}
		}
		if !found {
			t.Errorf("known difference %q (%s) no longer differs: delete the note", p, why)
		}
	}
}

func short2(v any) string {
	b, _ := json.Marshal(v)
	if len(b) > 1200 {
		return string(b[:1200]) + "..."
	}
	return string(b)
}

func TestRedfishInfoAgainstRealAnsible(t *testing.T) {
	ansible := requireAnsible(t)
	for _, tree := range []string{mockbmc.TreeRackmount, mockbmc.TreeEmulator} {
		for _, c := range redfishInfoInteropCases() {
			c.tree = tree
			cc := c.args["category"].([]any)[0].(string) + "/" + c.args["command"].([]any)[0].(string)
			c.knownDiff = redfishInfoKnownDiffs[tree+"/"+cc]
			name := tree + "/" + cc
			t.Run(name, func(t *testing.T) { runInteropCase(t, ansible, c) })
		}
	}
}

// redfishInfoInteropCases is every (category, command) of redfish_info.
func redfishInfoInteropCases() []interopCase {
	var cases []interopCase
	cats := []string{"Systems", "Chassis", "Accounts", "Sessions", "Update", "Manager", "Service"}
	for _, cat := range cats {
		for _, cmd := range redfishInfoCategories[cat] {
			cases = append(cases, interopCase{module: "redfish_info",
				args: map[string]any{"category": []any{cat}, "command": []any{cmd}}})
		}
	}
	return cases
}

// redfishInfoKnownDiffs: where the go-redfish backend and real ansible
// differ, tree/category/command -> path -> why. A row that is not listed is
// identical. Every entry is a place where real ansible and go-redfish part
// ways, and real is the oracle.
var redfishInfoKnownDiffs = map[string]map[string]string{
	"emulator-1u/Accounts/ListUsers": {
		".msg":           "real crashes (KeyError 'Accounts': the emulator's AccountService has no Accounts link, real indexes it); go-redfish also finds Links.Accounts",
		".redfish_facts": "go-redfish returns the user list where real fails",
		".failed":        "real: task failed",
	},
	"emulator-1u/Accounts/GetAccountServiceConfig": {
		".msg":           "real crashes (KeyError 'Accounts'), as for ListUsers",
		".redfish_facts": "go-redfish returns the AccountService where real fails",
		".failed":        "real: task failed",
	},
	"emulator-1u/Sessions/GetSessions": {
		".msg":           "real: SessionCollection resource not found (the root has no Links.Sessions); go-redfish falls back to the default Sessions path",
		".redfish_facts": "go-redfish returns an empty list where real fails",
		".failed":        "real: task failed",
	},
}
