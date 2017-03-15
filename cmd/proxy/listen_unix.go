// +build !windows

package main

import (
	"net"
	"syscall"
)

func listen(s string) (net.Listener, error) {
	syscall.Unlink(s)
	return net.Listen("unix", s)
}
