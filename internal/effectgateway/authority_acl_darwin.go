package effectgateway

import (
	"encoding/binary"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Darwin's extended ACL can grant access despite the mode bits. Ask the
// kernel for kauth_filesec on the same descriptor that was stat'ed. The
// attrlist and kauth_filesec layouts are defined in sys/attr.h and sys/kauth.h.
func authorityACLProtected(fd int) bool {
	attrs := struct {
		count, reserved              uint16
		common, vol, dir, file, fork uint32
	}{count: 5, common: unix.ATTR_CMN_EXTENDED_SECURITY}
	const (
		header     = 12 // length + attrreference_t
		filesec    = 44 // magic + owner and group UUIDs + ACL header
		maxEntries = 128
		entrySize  = 24 // UUID + flags + rights
	)
	var buf [header + filesec + maxEntries*entrySize]byte
	_, _, errno := unix.Syscall6(unix.SYS_FGETATTRLIST, uintptr(fd),
		uintptr(unsafe.Pointer(&attrs)), uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)), 0, 0)
	if errno != 0 {
		return false
	}
	u32 := binary.NativeEndian.Uint32
	total, offset, length := u32(buf[:4]), u32(buf[4:8]), u32(buf[8:12])
	if offset != 8 || total < header || total > uint32(len(buf)) || length != total-header {
		return false
	}
	if length == 0 {
		return true
	}
	if length < filesec || u32(buf[header:]) != 0x012cc16d { // KAUTH_FILESEC_MAGIC
		return false
	}
	count := u32(buf[header+36:])
	if count == ^uint32(0) { // KAUTH_FILESEC_NOACL
		return length == filesec
	}
	if count > maxEntries || length != filesec+count*entrySize {
		return false
	}
	for i := uint32(0); i < count; i++ {
		flags := u32(buf[header+filesec+i*entrySize+16:])
		if flags&0xf != 2 { // KAUTH_ACE_KINDMASK / KAUTH_ACE_DENY
			return false
		}
	}
	return true
}
