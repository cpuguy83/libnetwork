package main

import (
	"net"

	winio "github.com/Microsoft/go-winio"
)

const sddl = "D:P(A;;GA;;;BA)(A;;GA;;;SY)"

func listen(s string) (net.Listener, error) {
	c := winio.PipeConfig{
		SecurityDescriptor: sddl,
		MessageMode:        true,  // Use message mode so that CloseWrite() is supported
		InputBufferSize:    65536, // Use 64KB buffers to improve performance
		OutputBufferSize:   65536,
	}
	return winio.ListenPipe(s, &c)
}
