package effectgateway

import (
	"net"

	"golang.org/x/sys/unix"
)

func guardedExecutionPeer(conn *net.UnixConn, uid uint32, pid int) bool {
	raw, err := conn.SyscallConn()
	if err != nil {
		return false
	}
	var match bool
	err = raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		match = err == nil && cred.Uid == uid && int(cred.Pid) == pid
	})
	return err == nil && match
}
