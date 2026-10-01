//go:build !windows

package modules

import (
	"net"
	"syscall"
)

// wakeonlanEnableBroadcast sets SO_BROADCAST on uc's underlying socket
// — required before a UDP send to a broadcast address succeeds; without
// it the kernel refuses the send outright.
//
// This exists once per platform because syscall.SetsockoptInt takes a
// different first argument on each: an int file descriptor here, a
// syscall.Handle on Windows. The real module is a plain UDP socket with
// SO_BROADCAST set and nothing POSIX about it, so refusing to build on
// Windows over this one call would be this port's limitation, not the
// module's.
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
