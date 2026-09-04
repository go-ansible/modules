package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePacketVolumeAttachment implements Ansible's
// `packet_volume_attachment` (community.general) module. This port
// cannot implement it, for the same reason as packet_volume.go: real
// packet_volume_attachment.py attaches/detaches a classic Elastic
// Block Storage volume to a device via the packet-python SDK, and
// Equinix Metal's own current official CLI (`metal`) has no
// volume/storage subcommand at all — see packet_common.go's own doc
// comment (metalNoVolumeSupport) for what was checked. Every state
// (present/absent) Fails loud (Result{Failed:true}).
func modulePacketVolumeAttachment(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return metalNoVolumeSupport("packet_volume_attachment"), nil
}
