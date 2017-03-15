package rpc

import (
	"net"
	"strconv"
)

func Key(p *ProxySpec) string {
	s := net.JoinHostPort(p.Frontend.Addr, strconv.Itoa(int(p.Frontend.Port)))
	return s + "/" + p.Protocol
}
