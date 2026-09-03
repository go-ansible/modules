package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSNMPFactsScalarsAndInterfaces(t *testing.T) {
	scalarOut := "1.3.6.1.2.1.1.1.0 Linux ubuntu-user 4.4.0\n" +
		"1.3.6.1.2.1.1.2.0 1.3.6.1.4.1.8072.3.2.10\n" +
		"1.3.6.1.2.1.1.3.0 42388\n" +
		"1.3.6.1.2.1.1.4.0 Me <me@example.org>\n" +
		"1.3.6.1.2.1.1.5.0 ubuntu-user\n" +
		"1.3.6.1.2.1.1.6.0 Sitting on the Dock of the Bay\n"

	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v snmpget && command -v snmpwalk": {RC: 0},
		"snmpget -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.1.1.0 1.3.6.1.2.1.1.2.0 1.3.6.1.2.1.1.3.0 1.3.6.1.2.1.1.4.0 1.3.6.1.2.1.1.5.0 1.3.6.1.2.1.1.6.0": {RC: 0, Stdout: scalarOut},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.2.2.1.1":                                                                                        {RC: 0, Stdout: "1.3.6.1.2.1.2.2.1.1.1 1\n"},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.2.2.1.2":                                                                                        {RC: 0, Stdout: "1.3.6.1.2.1.2.2.1.2.1 lo\n"},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.2.2.1.4":                                                                                        {RC: 0, Stdout: "1.3.6.1.2.1.2.2.1.4.1 65536\n"},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.2.2.1.5":                                                                                        {RC: 0, Stdout: "1.3.6.1.2.1.2.2.1.5.1 65536\n"},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.2.2.1.6":                                                                                        {RC: 0, Stdout: "1.3.6.1.2.1.2.2.1.6.1 0x000a305a52a1\n"},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.2.2.1.7":                                                                                        {RC: 0, Stdout: "1.3.6.1.2.1.2.2.1.7.1 1\n"},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.2.2.1.8":                                                                                        {RC: 0, Stdout: "1.3.6.1.2.1.2.2.1.8.1 1\n"},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.31.1.1.1.18":                                                                                    {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.4.20.1.1":                                                                                       {RC: 0, Stdout: "1.3.6.1.2.1.4.20.1.1.127.0.0.1 127.0.0.1\n"},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.4.20.1.2":                                                                                       {RC: 0, Stdout: "1.3.6.1.2.1.4.20.1.2.127.0.0.1 1\n"},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.4.20.1.3":                                                                                       {RC: 0, Stdout: "1.3.6.1.2.1.4.20.1.3.127.0.0.1 255.0.0.0\n"},
	})

	res, err := moduleSNMPFacts(context.Background(), conn, map[string]any{
		"host": "10.0.0.1", "version": "v2c", "community": "public",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts := res.Extra["ansible_facts"].(map[string]any)
	if facts["ansible_sysname"] != "ubuntu-user" {
		t.Fatalf("ansible_sysname = %v", facts["ansible_sysname"])
	}
	if facts["ansible_sysuptime"] != "42388" {
		t.Fatalf("ansible_sysuptime = %v (want string)", facts["ansible_sysuptime"])
	}
	ifaces := facts["ansible_interfaces"].(map[string]any)
	iface1 := ifaces["1"].(map[string]any)
	if iface1["name"] != "lo" {
		t.Fatalf("iface1 = %+v", iface1)
	}
	if iface1["mac"] != "000a305a52a1" {
		t.Fatalf("mac = %v", iface1["mac"])
	}
	if iface1["adminstatus"] != "up" || iface1["operstatus"] != "up" {
		t.Fatalf("iface1 = %+v", iface1)
	}
	ipv4 := iface1["ipv4"].([]map[string]any)
	if len(ipv4) != 1 || ipv4[0]["address"] != "127.0.0.1" || ipv4[0]["netmask"] != "255.0.0.0" {
		t.Fatalf("ipv4 = %+v", ipv4)
	}
	all := facts["ansible_all_ipv4_addresses"].([]string)
	if len(all) != 1 || all[0] != "127.0.0.1" {
		t.Fatalf("all_ipv4_addresses = %v", all)
	}
}

func TestModuleSNMPFactsV2TreatedSameAsV2c(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v snmpget && command -v snmpwalk": {RC: 0},
		"snmpget -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.1.1.0 1.3.6.1.2.1.1.2.0 1.3.6.1.2.1.1.3.0 1.3.6.1.2.1.1.4.0 1.3.6.1.2.1.1.5.0 1.3.6.1.2.1.1.6.0": {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.2.2.1.1":                                                                                        {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.2.2.1.2":                                                                                        {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.2.2.1.4":                                                                                        {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.2.2.1.5":                                                                                        {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.2.2.1.6":                                                                                        {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.2.2.1.7":                                                                                        {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.2.2.1.8":                                                                                        {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.31.1.1.1.18":                                                                                    {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.4.20.1.1":                                                                                       {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.4.20.1.2":                                                                                       {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 2c -c public 10.0.0.1 1.3.6.1.2.1.4.20.1.3":                                                                                       {RC: 0, Stdout: ""},
	})
	res, err := moduleSNMPFacts(context.Background(), conn, map[string]any{
		"host": "10.0.0.1", "version": "v2", "community": "public",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleSNMPFactsMissingCommunity(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v snmpget && command -v snmpwalk": {RC: 0},
	})
	res, err := moduleSNMPFacts(context.Background(), conn, map[string]any{
		"host": "10.0.0.1", "version": "v2c",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when community is missing for v2c")
	}
}

func TestModuleSNMPFactsV3AuthPriv(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v snmpget && command -v snmpwalk": {RC: 0},
		"snmpget -O qn -v 3 -u snmp-user -l authPriv -a SHA -A abc12345 -x AES -X def6789 10.0.0.1 1.3.6.1.2.1.1.1.0 1.3.6.1.2.1.1.2.0 1.3.6.1.2.1.1.3.0 1.3.6.1.2.1.1.4.0 1.3.6.1.2.1.1.5.0 1.3.6.1.2.1.1.6.0": {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 3 -u snmp-user -l authPriv -a SHA -A abc12345 -x AES -X def6789 10.0.0.1 1.3.6.1.2.1.2.2.1.1":                                                                                        {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 3 -u snmp-user -l authPriv -a SHA -A abc12345 -x AES -X def6789 10.0.0.1 1.3.6.1.2.1.2.2.1.2":                                                                                        {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 3 -u snmp-user -l authPriv -a SHA -A abc12345 -x AES -X def6789 10.0.0.1 1.3.6.1.2.1.2.2.1.4":                                                                                        {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 3 -u snmp-user -l authPriv -a SHA -A abc12345 -x AES -X def6789 10.0.0.1 1.3.6.1.2.1.2.2.1.5":                                                                                        {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 3 -u snmp-user -l authPriv -a SHA -A abc12345 -x AES -X def6789 10.0.0.1 1.3.6.1.2.1.2.2.1.6":                                                                                        {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 3 -u snmp-user -l authPriv -a SHA -A abc12345 -x AES -X def6789 10.0.0.1 1.3.6.1.2.1.2.2.1.7":                                                                                        {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 3 -u snmp-user -l authPriv -a SHA -A abc12345 -x AES -X def6789 10.0.0.1 1.3.6.1.2.1.2.2.1.8":                                                                                        {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 3 -u snmp-user -l authPriv -a SHA -A abc12345 -x AES -X def6789 10.0.0.1 1.3.6.1.2.1.31.1.1.1.18":                                                                                    {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 3 -u snmp-user -l authPriv -a SHA -A abc12345 -x AES -X def6789 10.0.0.1 1.3.6.1.2.1.4.20.1.1":                                                                                       {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 3 -u snmp-user -l authPriv -a SHA -A abc12345 -x AES -X def6789 10.0.0.1 1.3.6.1.2.1.4.20.1.2":                                                                                       {RC: 0, Stdout: ""},
		"snmpwalk -O qn -v 3 -u snmp-user -l authPriv -a SHA -A abc12345 -x AES -X def6789 10.0.0.1 1.3.6.1.2.1.4.20.1.3":                                                                                       {RC: 0, Stdout: ""},
	})
	res, err := moduleSNMPFacts(context.Background(), conn, map[string]any{
		"host": "10.0.0.1", "version": "v3", "level": "authPriv",
		"integrity": "sha", "privacy": "aes", "username": "snmp-user",
		"authkey": "abc12345", "privkey": "def6789",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleSNMPFactsV3MissingPrivacy(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v snmpget && command -v snmpwalk": {RC: 0},
	})
	res, err := moduleSNMPFacts(context.Background(), conn, map[string]any{
		"host": "10.0.0.1", "version": "v3", "level": "authPriv",
		"integrity": "sha", "username": "snmp-user", "authkey": "abc12345",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when privacy/privkey missing for authPriv")
	}
}

func TestModuleSNMPFactsNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v snmpget && command -v snmpwalk": {RC: 1},
	})
	res, err := moduleSNMPFacts(context.Background(), conn, map[string]any{
		"host": "10.0.0.1", "version": "v2c", "community": "public",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when net-snmp tools are not on the target")
	}
}

func TestModuleSNMPFactsMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSNMPFacts(context.Background(), conn, map[string]any{"version": "v2c"}); err == nil {
		t.Fatal("want error for missing host")
	}
	if _, err := moduleSNMPFacts(context.Background(), conn, map[string]any{"host": "x"}); err == nil {
		t.Fatal("want error for missing version")
	}
}
