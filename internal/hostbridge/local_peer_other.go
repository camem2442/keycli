//go:build !linux && !darwin

package hostbridge

import (
	"net"
	"os"
)

func authorityPeerUID(*net.UnixConn, uint32) bool { return false }

func authoritySocketOwner(os.FileInfo, uint32) bool { return false }
