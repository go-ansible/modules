package modules

import (
	"context"
	"net"
	"strconv"
	"strings"
	"syscall"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleWakeonlan implements Ansible's `wakeonlan` module: sends a
// magic Wake-on-LAN (WoL) UDP broadcast packet for a given MAC address.
//
// Architectural note: real community.general.wakeonlan's own EXAMPLES
// (both of them) use `delegate_to: localhost`, and this is the only
// usage the doc shows — unlike mail.go's own real doc, which showed a
// genuine mix of on-target and delegated examples. That is also the
// only usage that makes sense for what this module does: it sends the
// magic packet to WAKE UP a machine that is, by definition, currently
// powered off, so there is no live target to run a remote command
// against in the first place. The packet has to originate from some
// OTHER, already-running host on the target's broadcast domain — the
// Ansible controller, per real wakeonlan's own documented pattern.
//
// Consistent with that, this port sends the UDP packet directly from
// wherever this Go function itself executes (i.e. the control node,
// same as every module's own Go logic per module.go's package doc
// comment) using the standard library's net package, rather than
// shelling out through `conn`. conn is accepted (for Func signature
// compatibility with every other module) but is intentionally never
// used — sending via conn.Exec would mean composing and running a
// UDP-broadcast one-liner against a target that, again, cannot
// possibly be up to receive or run it.
//
// Args: mac (string, required) — MAC address in any of the colon/
// hyphen/bare-hex forms real wakeonlan accepts; broadcast (string,
// default "255.255.255.255"); port (int, default 7).
//
// Real wakeonlan's own NOTES are equally true here: this module sends
// a magic packet without knowing whether it worked, and always reports
// Changed (there is no idempotent "already awake" check — matching
// real wakeonlan's own documented behavior, which has no check_mode-
// aware idempotency beyond "always sends"). Not implemented: real
// wakeonlan's SecureOn password support (its own doc lists this as a
// TODO, never shipped in real wakeonlan either).
func moduleWakeonlan(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	mac, err := requireString(args, "mac")
	if err != nil {
		return Result{}, err
	}
	broadcast := argString(args, "broadcast", "255.255.255.255")
	port := argInt(args, "port", 7)

	packet, err := wakeonlanMagicPacket(mac)
	if err != nil {
		return Result{}, err
	}

	pc, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return Result{}, err
	}
	defer pc.Close()
	uc := pc.(*net.UDPConn)

	if err := wakeonlanEnableBroadcast(uc); err != nil {
		return Result{}, err
	}

	addr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(broadcast, strconv.Itoa(port)))
	if err != nil {
		return Result{}, errArg("wakeonlan: invalid broadcast address %q: %v", broadcast, err)
	}
	if _, err := uc.WriteTo(packet, addr); err != nil {
		return Result{}, err
	}
	return Changed("sent WoL magic packet for " + mac + " to " + broadcast), nil
}

// wakeonlanMagicPacket builds the 102-byte WoL magic packet: 6 bytes of
// 0xFF followed by the target MAC repeated 16 times. mac may be given
// separated by ':' or '-', or as bare hex, matching real wakeonlan's
// own accepted forms.
func wakeonlanMagicPacket(mac string) ([]byte, error) {
	hex := strings.NewReplacer(":", "", "-", "", ".", "").Replace(mac)
	if len(hex) != 12 {
		return nil, errArg("wakeonlan: invalid mac address %q", mac)
	}
	macBytes := make([]byte, 6)
	for i := 0; i < 6; i++ {
		v, err := strconv.ParseUint(hex[i*2:i*2+2], 16, 8)
		if err != nil {
			return nil, errArg("wakeonlan: invalid mac address %q", mac)
		}
		macBytes[i] = byte(v)
	}
	packet := make([]byte, 0, 102)
	for i := 0; i < 6; i++ {
		packet = append(packet, 0xFF)
	}
	for i := 0; i < 16; i++ {
		packet = append(packet, macBytes...)
	}
	return packet, nil
}

// wakeonlanEnableBroadcast sets SO_BROADCAST on uc's underlying socket
// — required before a UDP send to a broadcast address succeeds; without
// it the kernel refuses the send outright.
func wakeonlanEnableBroadcast(uc *net.UDPConn) error {
	raw, err := uc.SyscallConn()
	if err != nil {
		return err
	}
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		sockErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
	}); err != nil {
		return err
	}
	return sockErr
}
