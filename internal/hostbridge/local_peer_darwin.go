package hostbridge

import (
	"net"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func authorityPeerUID(conn *net.UnixConn, expected uint32) bool {
	raw, err := conn.SyscallConn()
	if err != nil {
		return false
	}
	var match bool
	err = raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		match = err == nil && cred.Uid == expected
	})
	return err == nil && match
}

func authorityPeerProcess(conn *net.UnixConn, uid uint32, pid int) bool {
	raw, err := conn.SyscallConn()
	if err != nil {
		return false
	}
	var match bool
	err = raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		peerPID, pidErr := unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
		match = err == nil && pidErr == nil && cred.Uid == uid && peerPID == pid
	})
	return err == nil && match
}

func authoritySocketOwner(info os.FileInfo, expected uint32) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == expected
}
