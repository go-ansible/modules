package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHpiloInfo implements Ansible's `hpilo_info` module. Real
// hpilo_info.py talks RIBCL directly to the iLO's own network address;
// this port substitutes local/in-band `ilorest` exactly as
// hpilo_boot.go and the rest of this batch's ilo_redfish_* family do
// (see hpilo_common.go's own doc comment).
//
// # Field mapping
//
// hw_bios_version, hw_system_serial, hw_product_name, hw_product_uuid,
// and hw_uuid come from the Systems resource's own BiosVersion/
// SerialNumber/Model/UUID Redfish properties (walked via the same
// iloSystemURI helper ilo_redfish_command.go already uses, never a
// hardcoded "/1/" — see hpilo_common.go's own doc comment). Real RIBCL
// exposes hw_uuid and hw_product_uuid as two DISTINCT fields (System
// Information's UUID vs cUUID); Redfish's ComputerSystem resource has
// only one UUID property, so this port reports the same Redfish value
// for both — an honestly-documented shape difference, not two
// independently-verified values. hw_ethN/hw_eth_ilo come from walking
// Systems/{id}/EthernetInterfaces (host NICs, numbered by COLLECTION
// ORDER — real RIBCL numbers by a "Port" field this port has no
// equivalent for) and Managers/{id}/EthernetInterfaces (the iLO's own
// management NIC, matching real hw_eth_ilo's own intent). host_power_
// status reuses hpiloPowerState, exactly as hpilo_boot.go does.
//
// # What is NOT covered
//
// hw_bios_date, hw_health, hw_memory_total, and
// hw_memory_details_summary have no single, generation-stable Redfish
// resource path this port could verify (BIOS date usually lives in a
// vendor/generation-specific SoftwareInventory entry, and full
// health/memory rollups need a Chassis Thermal/Power/Memory walk this
// batch did not verify against real HPE documentation) — omitted
// rather than guessed; a caller reading these specific keys gets a
// missing key, not a wrong value.
//
// Args: host/login/password/ssl_version accepted for argument-shape
// compatibility but have NO EFFECT (see redfish_common.go's own doc
// comment on this batch's local/in-band CLI architecture).
func moduleHpiloInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if _, err := requireString(args, "host"); err != nil {
		return Result{}, err
	}

	if res, ok := hpiloRequireBinary(ctx, conn, "hpilo_info"); !ok {
		return res, nil
	}

	info := map[string]any{"module_hw": true}

	systemURI, err := iloSystemURI(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	if systemURI != "" {
		var sys struct {
			BiosVersion  string `json:"BiosVersion"`
			SerialNumber string `json:"SerialNumber"`
			Model        string `json:"Model"`
			UUID         string `json:"UUID"`
		}
		res, err := iloRawGet(ctx, conn, systemURI, &sys)
		if err != nil {
			return Result{}, err
		}
		if res.RC == 0 {
			if sys.BiosVersion != "" {
				info["hw_bios_version"] = sys.BiosVersion
			}
			if sys.SerialNumber != "" {
				info["hw_system_serial"] = sys.SerialNumber
			}
			if sys.Model != "" {
				info["hw_product_name"] = sys.Model
			}
			if sys.UUID != "" {
				info["hw_uuid"] = sys.UUID
				info["hw_product_uuid"] = sys.UUID
			}
		}

		if nics, err := hpiloEthernetInterfaces(ctx, conn, systemURI+"EthernetInterfaces/"); err != nil {
			return Result{}, err
		} else {
			for i, mac := range nics {
				info[hpiloEthKey(i)] = hpiloMacEntry(mac)
			}
		}
	}

	if managerURI, err := iloManagerURI(ctx, conn); err != nil {
		return Result{}, err
	} else if managerURI != "" {
		if nics, err := hpiloEthernetInterfaces(ctx, conn, managerURI+"EthernetInterfaces/"); err != nil {
			return Result{}, err
		} else if len(nics) > 0 {
			info["hw_eth_ilo"] = hpiloMacEntry(nics[0])
		}
	}

	powerStatus, _, err := hpiloPowerState(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	info["host_power_status"] = powerStatus

	res := Ok("")
	for k, v := range info {
		res = res.WithExtra(k, v)
	}
	return res, nil
}

func hpiloEthKey(i int) string {
	return "hw_eth" + strconv.Itoa(i)
}

func hpiloMacEntry(mac string) map[string]any {
	return map[string]any{"macaddress": mac, "macaddress_dash": strings.ReplaceAll(mac, ":", "-")}
}

// hpiloEthernetInterfaces walks a Redfish EthernetInterfaces collection
// and returns each member's MACAddress, in collection order.
func hpiloEthernetInterfaces(ctx context.Context, conn remoteexec.Connection, collectionURI string) ([]string, error) {
	var coll struct {
		Members []struct {
			ODataID string `json:"@odata.id"`
		} `json:"Members"`
	}
	res, err := iloRawGet(ctx, conn, collectionURI, &coll)
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, nil
	}
	var macs []string
	for _, m := range coll.Members {
		var nic struct {
			MACAddress string `json:"MACAddress"`
		}
		nres, err := iloRawGet(ctx, conn, m.ODataID, &nic)
		if err != nil {
			return nil, err
		}
		if nres.RC == 0 && nic.MACAddress != "" {
			macs = append(macs, nic.MACAddress)
		}
	}
	return macs, nil
}
