package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHpiloInfoGathersIdentityAndPower(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ilorest": {RC: 0},
		"ilorest rawget /redfish/v1/Systems/": {
			RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Systems/1/"}]}`,
		},
		"ilorest rawget /redfish/v1/Systems/1/": {
			RC: 0, Stdout: `{"BiosVersion":"P68","SerialNumber":"ABC12345D6","Model":"ProLiant DL360 G7","UUID":"ef50bac8-2845-40ff-81d9-675315501dac","PowerState":"On"}`,
		},
		"ilorest rawget /redfish/v1/Systems/1/EthernetInterfaces/": {
			RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Systems/1/EthernetInterfaces/1/"}]}`,
		},
		"ilorest rawget /redfish/v1/Systems/1/EthernetInterfaces/1/": {
			RC: 0, Stdout: `{"MACAddress":"00:11:22:33:44:55"}`,
		},
		"ilorest rawget /redfish/v1/Managers/": {
			RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Managers/1/"}]}`,
		},
		"ilorest rawget /redfish/v1/Managers/1/EthernetInterfaces/": {
			RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Managers/1/EthernetInterfaces/1/"}]}`,
		},
		"ilorest rawget /redfish/v1/Managers/1/EthernetInterfaces/1/": {
			RC: 0, Stdout: `{"MACAddress":"00:11:22:33:44:BA"}`,
		},
	})
	res, err := moduleHpiloInfo(context.Background(), conn, map[string]any{"host": "ilo.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["hw_system_serial"] != "ABC12345D6" {
		t.Fatalf("hw_system_serial = %v", res.Extra["hw_system_serial"])
	}
	if res.Extra["hw_product_name"] != "ProLiant DL360 G7" {
		t.Fatalf("hw_product_name = %v", res.Extra["hw_product_name"])
	}
	if res.Extra["hw_uuid"] != "ef50bac8-2845-40ff-81d9-675315501dac" {
		t.Fatalf("hw_uuid = %v", res.Extra["hw_uuid"])
	}
	if res.Extra["host_power_status"] != "ON" {
		t.Fatalf("host_power_status = %v", res.Extra["host_power_status"])
	}
	eth0, _ := res.Extra["hw_eth0"].(map[string]any)
	if eth0["macaddress"] != "00:11:22:33:44:55" || eth0["macaddress_dash"] != "00-11-22-33-44-55" {
		t.Fatalf("hw_eth0 = %+v", eth0)
	}
	ilo, _ := res.Extra["hw_eth_ilo"].(map[string]any)
	if ilo["macaddress"] != "00:11:22:33:44:BA" {
		t.Fatalf("hw_eth_ilo = %+v", ilo)
	}
}

func TestModuleHpiloInfoRequiresHost(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleHpiloInfo(context.Background(), conn, map[string]any{})
	if err == nil {
		t.Fatal("want error for missing host")
	}
}
