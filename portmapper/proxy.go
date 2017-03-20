package portmapper

import (
	"context"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/docker/libnetwork/api/proxy"
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
	client       proxy.ProxyClient
}

func newProxy(pm *PortMapper, proto string, frontendIP net.IP, frontendPort int, backendIP net.IP, backendPort int) userlandProxy {
	m := proxyMapping{
		proto:        proto,
		frontendIP:   frontendIP,
		frontendPort: frontendPort,
		backendIP:    backendIP,
		backendPort:  backendPort,
		client:       pm.proxyService,
	}
	if !pm.proxyBackend {
		m.backendIP = nil
		m.backendPort = 0
	}

	return m
}

func (m proxyMapping) toProxySpec() *proxy.ProxySpec {
	spec := &proxy.ProxySpec{
		Protocol: m.proto,
		Frontend: &proxy.ProxySpec_EndpointSpec{
			Addr: m.frontendIP.String(),
			Port: uint32(m.frontendPort),
		},
	}

	if m.backendIP != nil {
		spec.Backend = &proxy.ProxySpec_EndpointSpec{
			Addr: m.backendIP.String(),
			Port: uint32(m.backendPort),
		}
	}
	return spec
}

func (m proxyMapping) Start() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := m.client.StartProxy(ctx, &proxy.StartProxyRequest{
		Spec: m.toProxySpec(),
	})
	return errors.Wrap(err, "error starting proxy")
}

func (m proxyMapping) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := m.client.StopProxy(ctx, &proxy.StopProxyRequest{
		Spec: m.toProxySpec(),
	})
	return errors.Wrap(err, "error starting proxy")
}

// ProxyCommand wraps an exec.Cmd to run the userland TCP and UDP
// proxies as separate processes.
type ProxyCommand struct {
	cmd      *exec.Cmd
	sockPath string
	conn     *grpc.ClientConn
	client   proxy.ProxyClient
}

type ProxyService interface {
	Run() error
	Stop() error
	Client() proxy.ProxyClient
}

func (p *ProxyCommand) Run() error {
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

	p.client = proxy.NewProxyClient(p.conn)
	return nil
}

func (p *ProxyCommand) Stop() error {
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

func (p *ProxyCommand) Client() proxy.ProxyClient {
	return p.client
}

func NewProxyService(proxyPath, sockPath string) (ProxyService, error) {
	proxyCmd, err := newProxyCommand(proxyPath, sockPath)
	if err == nil {
		err = proxyCmd.Run()
	}
	return proxyCmd, errors.Wrap(err, "error setting up userland proxy option")
}
