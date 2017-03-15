package main

import (
	"fmt"
	"io"
	"net"
)

// dummyProxy just listen on some port, it is needed to prevent accidental
// port allocations on bound port, because without userland proxy we using
// iptables rules and not net.Listen
type dummyProxy struct {
	listener io.Closer
	addr     net.Addr
}

func (p *dummyProxy) Run() {
}

func (p *dummyProxy) Close() {
	p.listener.Close()
}

func (p *dummyProxy) FrontendAddr() net.Addr {
	return p.addr
}

func (p *dummyProxy) BackendAddr() net.Addr {
	return nil
}

func NewDummyProxy(frontendAddr net.Addr) (Proxy, error) {
	p := &dummyProxy{addr: frontendAddr}
	switch addr := frontendAddr.(type) {
	case *net.TCPAddr:
		l, err := net.ListenTCP("tcp", addr)
		if err != nil {
			return nil, err
		}
		p.listener = l
	case *net.UDPAddr:
		l, err := net.ListenUDP("udp", addr)
		if err != nil {
			return nil, err
		}
		p.listener = l
	default:
		return nil, fmt.Errorf("unknown addr type: %T", addr)
	}
	return p, nil
}
