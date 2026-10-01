//go:build windows

package modules

import (
	"net"
	"syscall"
)

// wakeonlanEnableBroadcast sets SO_BROADCAST on uc's underlying socket.
// See the !windows build of this file for why it is split: Windows
// types a socket as syscall.Handle rather than an int fd.
func wakeonlanEnableBroadcast(uc *net.UDPConn) error {
	raw, err := uc.SyscallConn()
	if err != nil {
		return err
	}
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		sockErr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
	}); err != nil {
		return err
	}
	return sockErr
}
