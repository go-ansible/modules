package modules

import "testing"

func TestSplitDN(t *testing.T) {
	got := splitDN(`uid=jdoe,ou=people,dc=example,dc=com`)
	want := []string{"uid=jdoe", "ou=people", "dc=example", "dc=com"}
	if len(got) != len(want) {
		t.Fatalf("splitDN = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("splitDN[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSplitDNEscapedComma(t *testing.T) {
	got := splitDN(`cn=Doe\, John,dc=example,dc=com`)
	want := []string{`cn=Doe\, John`, "dc=example", "dc=com"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("splitDN = %v, want %v", got, want)
	}
}

func TestLdapOrderValues(t *testing.T) {
	got := ldapOrderValues([]string{"{5}foo", "bar", "{0}baz"})
	want := []string{"{0}foo", "{1}bar", "{2}baz"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ldapOrderValues[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLdifAttrLineSafe(t *testing.T) {
	if got := ldifAttrLine("cn", []byte("Barbara Jensen")); got != "cn: Barbara Jensen" {
		t.Fatalf("ldifAttrLine = %q", got)
	}
}

func TestLdifAttrLineUnsafe(t *testing.T) {
	got := ldifAttrLine("jpegPhoto", []byte{0xff, 0xd8, 0xff})
	if got != "jpegPhoto:: /9j/" {
		t.Fatalf("ldifAttrLine = %q", got)
	}
}

func TestLdifAttrLineLeadingSpace(t *testing.T) {
	got := ldifAttrLine("cn", []byte(" leading space"))
	if got != "cn:: IGxlYWRpbmcgc3BhY2U=" {
		t.Fatalf("ldifAttrLine = %q", got)
	}
}

func TestParseLdif(t *testing.T) {
	out := "dn: uid=jdoe,dc=example,dc=com\n" +
		"cn: John Doe\n" +
		"objectClass: person\n" +
		"objectClass: inetOrgPerson\n" +
		"\n" +
		"dn: uid=asmith,dc=example,dc=com\n" +
		"cn: Alice Smith\n" +
		"\n"
	entries := parseLdif(out)
	if len(entries) != 2 {
		t.Fatalf("parseLdif = %d entries, want 2", len(entries))
	}
	if entries[0].dn != "uid=jdoe,dc=example,dc=com" {
		t.Fatalf("entries[0].dn = %q", entries[0].dn)
	}
	if got := entries[0].valuesOf("cn"); len(got) != 1 || got[0] != "John Doe" {
		t.Fatalf("cn = %v", got)
	}
	if got := entries[0].valuesOf("objectClass"); len(got) != 2 {
		t.Fatalf("objectClass = %v", got)
	}
	if got := entries[0].valuesOf("OBJECTCLASS"); len(got) != 2 {
		t.Fatalf("case-insensitive lookup failed: %v", got)
	}
	if entries[1].dn != "uid=asmith,dc=example,dc=com" {
		t.Fatalf("entries[1].dn = %q", entries[1].dn)
	}
}

func TestParseLdifBase64Value(t *testing.T) {
	// "hi" base64-encoded is "aGk="
	out := "dn: cn=x,dc=example,dc=com\ndescription:: aGk=\n\n"
	entries := parseLdif(out)
	if len(entries) != 1 {
		t.Fatalf("parseLdif = %d entries", len(entries))
	}
	if got := entries[0].valuesOf("description"); len(got) != 1 || got[0] != "hi" {
		t.Fatalf("description = %v", got)
	}
}

func TestParseLdifAttrsOnly(t *testing.T) {
	out := "dn: cn=admin,dc=example,dc=com\nobjectClass:\ndescription:\n\n"
	entries := parseLdif(out)
	if len(entries) != 1 {
		t.Fatalf("parseLdif = %d entries", len(entries))
	}
	if _, ok := entries[0].attrs["objectClass"]; !ok {
		t.Fatalf("attrs = %v, want objectClass key present", entries[0].attrs)
	}
}

func TestParseLdifSkipsComments(t *testing.T) {
	out := "# a comment\ndn: cn=x,dc=example,dc=com\ncn: x\n\n"
	entries := parseLdif(out)
	if len(entries) != 1 || entries[0].dn != "cn=x,dc=example,dc=com" {
		t.Fatalf("parseLdif = %+v", entries)
	}
}

func TestLdapIsBinaryAttr(t *testing.T) {
	binarySet := map[string]bool{"myphoto": true}
	if !ldapIsBinaryAttr("myPhoto", false, binarySet) {
		t.Fatal("want myPhoto binary via binary_attributes (case-insensitive)")
	}
	if ldapIsBinaryAttr("description", false, binarySet) {
		t.Fatal("description should not be binary")
	}
	if !ldapIsBinaryAttr("cACertificate;binary", true, map[string]bool{}) {
		t.Fatal("want ;binary option honored when honor_binary=true")
	}
	if ldapIsBinaryAttr("cACertificate;binary", false, map[string]bool{}) {
		t.Fatal("want ;binary option ignored when honor_binary=false")
	}
}

func TestLdapNormalizeAttrValuesBinary(t *testing.T) {
	got, err := ldapNormalizeAttrValues("aGVsbG8=", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || string(got[0]) != "hello" {
		t.Fatalf("got = %v", got)
	}
}

func TestLdapNormalizeAttrValuesBadBase64(t *testing.T) {
	if _, err := ldapNormalizeAttrValues("not-valid-base64!!", true, false); err == nil {
		t.Fatal("want error for invalid base64")
	}
}

func TestLdapNormalizeAttrValuesOrdered(t *testing.T) {
	got, err := ldapNormalizeAttrValues([]any{"a", "b"}, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(got[0]) != "{0}a" || string(got[1]) != "{1}b" {
		t.Fatalf("got = %v", got)
	}
}

func TestUtf8Replace(t *testing.T) {
	if got := utf8Replace("clean"); got != "clean" {
		t.Fatalf("utf8Replace = %q", got)
	}
	got := utf8Replace(string([]byte{0xff, 0xfe}))
	if got != "��" {
		t.Fatalf("utf8Replace = %q", got)
	}
}

func TestSortedKeys(t *testing.T) {
	got := sortedKeys(map[string]any{"z": 1, "a": 2, "m": 3})
	want := []string{"a", "m", "z"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortedKeys = %v, want %v", got, want)
		}
	}
}

func TestParseLdapConnBindDN(t *testing.T) {
	// bind_dn omitted entirely.
	c, err := parseLdapConn(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if c.bindDNSet {
		t.Fatal("want bindDNSet=false when bind_dn omitted")
	}

	// bind_dn present but empty -> anonymous simple bind.
	c, err = parseLdapConn(map[string]any{"bind_dn": ""})
	if err != nil {
		t.Fatal(err)
	}
	if !c.bindDNSet || c.bindDN != "" {
		t.Fatalf("c = %+v", c)
	}

	// bind_dn present and non-empty.
	c, err = parseLdapConn(map[string]any{"bind_dn": "cn=admin,dc=example,dc=com"})
	if err != nil {
		t.Fatal(err)
	}
	if !c.bindDNSet || c.bindDN != "cn=admin,dc=example,dc=com" {
		t.Fatalf("c = %+v", c)
	}
}

func TestParseLdapConnInvalidChoices(t *testing.T) {
	if _, err := parseLdapConn(map[string]any{"referrals_chasing": "bogus"}); err == nil {
		t.Fatal("want error for invalid referrals_chasing")
	}
	if _, err := parseLdapConn(map[string]any{"sasl_class": "bogus"}); err == nil {
		t.Fatal("want error for invalid sasl_class")
	}
	if _, err := parseLdapConn(map[string]any{"xorder_discovery": "bogus"}); err == nil {
		t.Fatal("want error for invalid xorder_discovery")
	}
	if _, err := parseLdapConn(map[string]any{"client_cert": "/a.pem"}); err == nil {
		t.Fatal("want error when client_cert given without client_key")
	}
}

func TestLdapEnvPrefix(t *testing.T) {
	c, err := parseLdapConn(map[string]any{"validate_certs": false, "ca_path": "/etc/ca.pem"})
	if err != nil {
		t.Fatal(err)
	}
	got := c.envPrefix()
	want := "LDAPTLS_REQCERT=never LDAPTLS_CACERT=/etc/ca.pem "
	if got != want {
		t.Fatalf("envPrefix = %q, want %q", got, want)
	}
}

func TestLdapResolveDNDisabled(t *testing.T) {
	c, err := parseLdapConn(map[string]any{"xorder_discovery": "disable"})
	if err != nil {
		t.Fatal(err)
	}
	fc := newFakeConn(nil)
	got := ldapResolveDN(nil, fc, c, nil, "olcDatabase={1}hdb,cn=config")
	if got != "olcDatabase={1}hdb,cn=config" {
		t.Fatalf("got = %q", got)
	}
	if len(fc.Commands) != 0 {
		t.Fatalf("xorder_discovery=disable must not run any search, got %v", fc.Commands)
	}
}
