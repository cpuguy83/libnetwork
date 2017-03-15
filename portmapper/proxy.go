package portmapper

import (
	"context"
	"io/ioutil"
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

func newProxy(pm *PortMapper, proto string, frontendIP net.IP, frontendPort int, backendIP net.IP, backendPort int) userlandProxy {
	m := proxyMapping{
		proto:        proto,
		frontendIP:   frontendIP,
		frontendPort: frontendPort,
		backendIP:    backendIP,
		backendPort:  backendPort,
		client:       pm.proxyCmd.client,
	}
	if !pm.enableUserlandProxy {
		m.backendIP = nil
		m.backendPort = 0
	}

	return m
}

func (m proxyMapping) toProxySpec() *rpc.ProxySpec {
	spec := &rpc.ProxySpec{
		Protocol: m.proto,
		Frontend: &rpc.ProxySpec_HostSpec{
			Addr: m.frontendIP.String(),
			Port: uint32(m.frontendPort),
		},
	}

	if m.backendIP != nil {
		spec.Backend = &rpc.ProxySpec_HostSpec{
			Addr: m.backendIP.String(),
			Port: uint32(m.backendPort),
		}
	}
	return spec
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

func setupUserlandProxy(proxyPath string) (*proxyCommand, error) {
	f, err := ioutil.TempFile("", "docker-proxy")
	if err != nil {
		return nil, errors.Wrap(err, "error creating proxy socket")
	}
	proxyCmd, err := newProxyCommand(proxyPath, f.Name())
	f.Close()
	if err == nil {
		err = proxyCmd.Run()
	}

	return proxyCmd, errors.Wrap(err, "error setting up userland proxy option")
}
