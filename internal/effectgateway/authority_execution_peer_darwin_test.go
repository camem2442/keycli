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
		cred, credErr := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		peerPID, pidErr := unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
		match = credErr == nil && pidErr == nil && cred.Uid == uid && peerPID == pid
	})
	return err == nil && match
}
