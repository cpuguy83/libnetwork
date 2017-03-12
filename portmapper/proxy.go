package portmapper

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/docker/libnetwork/cmd/proxy/rpc"
	"github.com/pkg/errors"

	"google.golang.org/grpc"
)

const userlandProxyCommandName = "docker-proxy"

type userlandProxy interface {
	Start() error
	Stop() error
}

type proxyMapping struct {
	proto        string
	frontendIP   net.IP
	frontendPort int
	backendIP    net.IP
	backendPort  int
	client       rpc.ProxyClient
}

func newProxy(client rpc.ProxyClient, proto string, frontendIP net.IP, frontendPort int, backendIP net.IP, backendPort int) userlandProxy {
	m := proxyMapping{
		proto:        proto,
		frontendIP:   frontendIP,
		frontendPort: frontendPort,
		backendIP:    backendIP,
		backendPort:  backendPort,
		client:       client,
	}
	return m
}

func (m proxyMapping) toProxySpec() *rpc.ProxySpec {
	return &rpc.ProxySpec{
		Protocol: m.proto,
		Frontend: &rpc.ProxySpec_HostSpec{
			Addr: m.frontendIP.String(),
			Port: uint32(m.frontendPort),
		},
		Backend: &rpc.ProxySpec_HostSpec{
			Addr: m.backendIP.String(),
			Port: uint32(m.backendPort),
		},
	}
}

func (m proxyMapping) Start() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := m.client.StartProxy(ctx, &rpc.StartProxyRequest{
		Spec: m.toProxySpec(),
	})
	return errors.Wrap(err, "error starting proxy")
}

func (m proxyMapping) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := m.client.StopProxy(ctx, &rpc.StopProxyRequest{
		Spec: m.toProxySpec(),
	})
	return errors.Wrap(err, "error starting proxy")
}

// proxyCommand wraps an exec.Cmd to run the userland TCP and UDP
// proxies as separate processes.
type proxyCommand struct {
	cmd      *exec.Cmd
	sockPath string
	conn     *grpc.ClientConn
	client   rpc.ProxyClient
}

func (p *proxyCommand) Run() error {
	var err error

	if err := p.cmd.Start(); err != nil {
		return errors.Wrap(err, "error starting docker-proxy")
	}

	p.conn, err = grpc.Dial(p.sockPath, grpc.WithBlock(), grpc.WithTimeout(16*time.Second), grpc.WithInsecure(), grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout("unix", addr, timeout)
	}))

	if err != nil {
		return errors.Wrap(err, "error connecting to socket")
	}

	p.client = rpc.NewProxyClient(p.conn)
	return nil
}

func (p *proxyCommand) Stop() error {
	p.conn.Close()
	if p.cmd.Process != nil {
		if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
			return err
		}
		return p.cmd.Wait()
	}
	err := os.Remove(p.sockPath)
	return errors.Wrap(err, "error cleaning up docker-proxy socket")
}

// dummyProxy just listen on some port, it is needed to prevent accidental
// port allocations on bound port, because without userland proxy we using
// iptables rules and not net.Listen
type dummyProxy struct {
	listener io.Closer
	addr     net.Addr
}

func newDummyProxy(proto string, hostIP net.IP, hostPort int) userlandProxy {
	switch proto {
	case "tcp":
		addr := &net.TCPAddr{IP: hostIP, Port: hostPort}
		return &dummyProxy{addr: addr}
	case "udp":
		addr := &net.UDPAddr{IP: hostIP, Port: hostPort}
		return &dummyProxy{addr: addr}
	}
	return nil
}

func (p *dummyProxy) Start() error {
	switch addr := p.addr.(type) {
	case *net.TCPAddr:
		l, err := net.ListenTCP("tcp", addr)
		if err != nil {
			return err
		}
		p.listener = l
	case *net.UDPAddr:
		l, err := net.ListenUDP("udp", addr)
		if err != nil {
			return err
		}
		p.listener = l
	default:
		return fmt.Errorf("Unknown addr type: %T", p.addr)
	}
	return nil
}

func (p *dummyProxy) Stop() error {
	if p.listener != nil {
		return p.listener.Close()
	}
	return nil
}
