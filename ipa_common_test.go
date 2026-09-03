package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestIpaParseRaw(t *testing.T) {
	out := "  dn: uid=pinky,cn=users,cn=accounts,dc=example,dc=com\n" +
		"  uid: pinky\n" +
		"  givenname: Pinky\n" +
		"  mail: pinky@acme.com\n" +
		"  mail: pinky2@acme.com\n" +
		"1 user matched\n" +
		"----------------------------\n" +
		"Number of entries returned 1\n" +
		"----------------------------\n"
	attrs := ipaParseRaw(out)
	if attrs["uid"][0] != "pinky" {
		t.Fatalf("uid = %v", attrs["uid"])
	}
	if len(attrs["mail"]) != 2 {
		t.Fatalf("mail = %v", attrs["mail"])
	}
	if _, ok := attrs["1 user matched"]; ok {
		t.Fatalf("summary line was mis-parsed as an attribute: %v", attrs)
	}
}

func TestIpaMemberKind(t *testing.T) {
	cases := []struct{ dn, kind, name string }{
		{"uid=alice,cn=users,cn=accounts,dc=example,dc=com", "user", "alice"},
		{"fqdn=host01.example.com,cn=computers,cn=accounts,dc=example,dc=com", "host", "host01.example.com"},
		{"cn=developers,cn=groups,cn=accounts,dc=example,dc=com", "cn", "developers"},
		{"krbprincipalname=http/host01.example.com@EXAMPLE.COM,cn=services,cn=accounts,dc=example,dc=com", "service", "http/host01.example.com@EXAMPLE.COM"},
		{"sudocmd=/bin/ls,cn=sudocmds,cn=sudo,dc=example,dc=com", "sudocmd", "/bin/ls"},
	}
	for _, c := range cases {
		kind, name := ipaMemberKind(c.dn)
		if kind != c.kind || name != c.name {
			t.Fatalf("ipaMemberKind(%q) = (%q, %q), want (%q, %q)", c.dn, kind, name, c.kind, c.name)
		}
	}
}

func TestIpaReconcileMembers(t *testing.T) {
	toAdd, toRemove := ipaReconcileMembers([]string{"alice", "bob"}, []string{"bob", "carol"}, true)
	if len(toAdd) != 1 || toAdd[0] != "carol" {
		t.Fatalf("toAdd = %v", toAdd)
	}
	if len(toRemove) != 1 || toRemove[0] != "alice" {
		t.Fatalf("toRemove = %v", toRemove)
	}

	toAdd, toRemove = ipaReconcileMembers([]string{"alice"}, []string{"alice", "bob"}, false)
	if len(toAdd) != 1 || toAdd[0] != "bob" {
		t.Fatalf("toAdd = %v", toAdd)
	}
	if len(toRemove) != 0 {
		t.Fatalf("toRemove = %v, want none (pruneExtra=false)", toRemove)
	}
}

func TestIpaScalarDiff(t *testing.T) {
	current := map[string][]string{"gidnumber": {"100"}}
	if _, has := ipaScalarDiff(map[string]any{}, "gidnumber", "gidnumber", "gidnumber", current); has {
		t.Fatal("want no diff when arg is omitted")
	}
	if flag, has := ipaScalarDiff(map[string]any{"gidnumber": "100"}, "gidnumber", "gidnumber", "gidnumber", current); has {
		t.Fatalf("want no diff when value matches, got %q", flag)
	}
	flag, has := ipaScalarDiff(map[string]any{"gidnumber": "200"}, "gidnumber", "gidnumber", "gidnumber", current)
	if !has || flag != "--gidnumber=200" {
		t.Fatalf("flag = %q, has = %v", flag, has)
	}
}

func TestIpaListDiff(t *testing.T) {
	current := map[string][]string{"mail": {"a@x.com", "b@x.com"}}
	if _, has := ipaListDiff(map[string]any{}, "mail", "mail", "mail", current); has {
		t.Fatal("want no diff when arg omitted")
	}
	if _, has := ipaListDiff(map[string]any{"mail": []any{"b@x.com", "a@x.com"}}, "mail", "mail", "mail", current); has {
		t.Fatal("want no diff when set matches regardless of order")
	}
	flags, has := ipaListDiff(map[string]any{"mail": []any{}}, "mail", "mail", "mail", current)
	if !has || len(flags) != 1 || flags[0] != "--mail=" {
		t.Fatalf("flags = %v, has = %v, want clearing token", flags, has)
	}
	flags, has = ipaListDiff(map[string]any{"mail": []any{"c@x.com"}}, "mail", "mail", "mail", current)
	if !has || len(flags) != 1 || flags[0] != "--mail=c@x.com" {
		t.Fatalf("flags = %v", flags)
	}
}

func TestIpaBoolDiff(t *testing.T) {
	current := map[string][]string{"idnsallowsyncptr": {"TRUE"}}
	if _, has := ipaBoolDiff(map[string]any{}, "allowsyncptr", "idnsallowsyncptr", "idnsallowsyncptr", current); has {
		t.Fatal("want no diff when arg omitted")
	}
	if _, has := ipaBoolDiff(map[string]any{"allowsyncptr": true}, "allowsyncptr", "idnsallowsyncptr", "idnsallowsyncptr", current); has {
		t.Fatal("want no diff when TRUE already matches Go true (case-insensitive)")
	}
	flag, has := ipaBoolDiff(map[string]any{"allowsyncptr": false}, "allowsyncptr", "idnsallowsyncptr", "idnsallowsyncptr", current)
	if !has || flag != "--idnsallowsyncptr=FALSE" {
		t.Fatalf("flag = %q, has = %v", flag, has)
	}
}

func TestIpaFlagRepeat(t *testing.T) {
	got := ipaFlagRepeat("user", []string{"alice", "bob"})
	want := []string{"--user=alice", "--user=bob"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got = %v, want %v", got, want)
		}
	}
}

func TestIpaShowAbsent(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"ipa user-show ghost --all --raw": {RC: 2, Stderr: "ipa: ERROR: ghost: user not found"},
	})
	attrs, present, err := ipaShow(context.Background(), fc, "user", "ghost")
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("want present=false for a nonzero exit")
	}
	if attrs != nil {
		t.Fatalf("attrs = %v, want nil", attrs)
	}
}

func TestIpaShowPresent(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"ipa user-show pinky --all --raw": {RC: 0, Stdout: "  uid: pinky\n  givenname: Pinky\n"},
	})
	attrs, present, err := ipaShow(context.Background(), fc, "user", "pinky")
	if err != nil {
		t.Fatal(err)
	}
	if !present {
		t.Fatal("want present=true")
	}
	if attrs["uid"][0] != "pinky" {
		t.Fatalf("attrs = %v", attrs)
	}
}

func TestIpaRequireBinaryMissing(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 1},
	})
	res, ok := ipaRequireBinary(context.Background(), fc, "ipa_user")
	if ok {
		t.Fatal("want ok=false when ipa binary is missing")
	}
	if !res.Failed {
		t.Fatal("want Failed result")
	}
}
