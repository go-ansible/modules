package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePacketVolume implements Ansible's `packet_volume`
// (community.general) module. This port cannot implement it: real
// packet_volume.py manages Equinix Metal's classic Elastic Block
// Storage ("volumes") product via the packet-python SDK, and Equinix
// Metal's own current official CLI (`metal`) has NO volume/storage
// subcommand anywhere in its command tree and no generic
// API-passthrough fallback either — a confirmed, real gap in the
// target platform's own tooling, not a guess. See packet_common.go's
// own doc comment (specifically metalNoVolumeSupport) for exactly what
// was checked and how. Every state (present/absent) Fails loud
// (Result{Failed:true}), per this batch's own explicit instructions
// for exactly this situation: fail loud rather than silently fake
// parity when a real capability genuinely has no CLI equivalent.
func modulePacketVolume(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return metalNoVolumeSupport("packet_volume"), nil
}
