package main

import (
	"errors"
	"flag"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/Sirupsen/logrus"
	"github.com/docker/libnetwork/cmd/proxy/rpc"

	pprofapi "github.com/cpuguy83/go-grpc-pprof/api"
	pprofgrpc "github.com/cpuguy83/go-grpc-pprof/server"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func runSrv(args []string) {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	logLevel := flags.String("log-level", "info", "set the log level for logging output")

	addr := flags.String("sock", "", "path to unix socket to listen on")
	flags.Parse(args)

	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logrus.Fatal(err)
	}
	logrus.SetLevel(level)

	if *addr == "" {
		errorOut("Must provide the sock flag")
		return
	}

	l, err := listen(*addr)
	if err != nil {
		logrus.Fatalf("error starting grpc listener: %v", err)
	}
	grpcsrv := grpc.NewServer(grpc.UnaryInterceptor(middleware))
	srv := &server{
		proxies: make(map[string]Proxy),
	}

	rpc.RegisterProxyServer(grpcsrv, srv)
	pprofapi.RegisterPProfServiceServer(grpcsrv, pprofgrpc.NewServer())

	go func() {
		s := make(chan os.Signal, 10)
		signal.Notify(s, os.Interrupt, syscall.SIGTERM)

		for range s {
			srv.shutdown()
			grpcsrv.GracefulStop()
		}
		os.Exit(0)
	}()

	grpcsrv.Serve(l)
}

type server struct {
	mu      sync.Mutex
	rpc     *grpc.Server
	proxies map[string]Proxy
}

func middleware(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	l := logrus.WithField("request", req).WithField("endpoint", info.FullMethod)
	l.Debug("begin")
	resp, err = handler(ctx, req)
	if err != nil {
		l.WithError(err).Error("error during request")
	}
	l.WithField("resp", resp).Debug("end")
	return resp, err
}

func (s *server) shutdown() {
	s.mu.Lock()

	for _, p := range s.proxies {
		p.Close()
	}

	s.mu.Unlock()
}

// StartProxy implements the StartProxy() call for the grpc ProxyServer type defined in the imported rpc package.
// It starts up a proxy for the spec defined in the request.
func (s *server) StartProxy(ctx context.Context, req *rpc.StartProxyRequest) (*rpc.StartProxyResponse, error) {
	frontend, backend, err := getProxyAddrs(req.Spec)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.proxies[rpc.Key(req.Spec)]; exists {
		return nil, grpc.Errorf(codes.FailedPrecondition, "proxy for %q already exists", rpc.Key(req.Spec))
	}

	if req.Spec.Backend == nil {
		logrus.Info("no backend")
		backend = nil
	}

	p, err := NewProxy(frontend, backend)
	if err != nil {
		return nil, err
	}
	if p == nil {
		panic("hmmm")
	}
	s.proxies[rpc.Key(req.Spec)] = p
	go p.Run()
	return &rpc.StartProxyResponse{}, nil
}

func getProxyAddrs(spec *rpc.ProxySpec) (frontend, backend net.Addr, err error) {
	if spec.Frontend == nil {
		return nil, nil, errors.New("missing frontend spec")
	}
	switch spec.Protocol {
	case "tcp":
		frontend = &net.TCPAddr{IP: net.ParseIP(spec.Frontend.Addr), Port: int(spec.Frontend.Port)}
		if spec.Backend != nil {
			backend = &net.TCPAddr{IP: net.ParseIP(spec.Backend.Addr), Port: int(spec.Backend.Port)}
		}
	case "udp":
		frontend = &net.UDPAddr{IP: net.ParseIP(spec.Frontend.Addr), Port: int(spec.Frontend.Port)}
		if spec.Backend != nil {
			backend = &net.UDPAddr{IP: net.ParseIP(spec.Backend.Addr), Port: int(spec.Backend.Port)}
		}
	default:
		return nil, nil, grpc.Errorf(codes.InvalidArgument, "unsupported proxy protocol: %s", spec.Protocol)
	}
	return
}

// StopProxy implements the StopProxy() call for the grpc ProxyServer type defined in the imported rpc package.
// It shuts down the proxy for the spec defined in the request.
func (s *server) StopProxy(ctx context.Context, req *rpc.StopProxyRequest) (*rpc.StopProxyResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p := s.proxies[rpc.Key(req.Spec)]
	if p != nil {
		p.Close()
		delete(s.proxies, rpc.Key(req.Spec))
	}

	return &rpc.StopProxyResponse{}, nil
}

func splitAddr(a net.Addr) (ip net.IP, port int) {
	if a == nil {
		return
	}
	switch aa := a.(type) {
	case *net.TCPAddr:
		ip = aa.IP
		port = aa.Port
	case *net.UDPAddr:
		ip = aa.IP
		port = aa.Port
	}

	return ip, port
}

func proxyToSpec(p Proxy) *rpc.ProxySpec {
	frontIP, frontPort := splitAddr(p.FrontendAddr())
	backIP, backPort := splitAddr(p.BackendAddr())
	spec := &rpc.ProxySpec{
		Protocol: p.FrontendAddr().Network(),
		Frontend: &rpc.ProxySpec_HostSpec{
			Addr: frontIP.String(),
			Port: uint32(frontPort),
		},
	}
	if backIP != nil {
		spec.Backend = &rpc.ProxySpec_HostSpec{
			Addr: backIP.String(),
			Port: uint32(backPort),
		}
	}
	return spec
}

// List implements the List() call for the grpc ProxyServer type defined int he imported rpc package.
// It lists all the currently running proxies.
func (s *server) List(Ctx context.Context, req *rpc.ListRequest) (*rpc.ListResponse, error) {
	s.mu.Lock()

	ls := make([]*rpc.ProxySpec, 0, len(s.proxies))
	for _, p := range s.proxies {
		ls = append(ls, proxyToSpec(p))
	}
	s.mu.Unlock()
	return &rpc.ListResponse{Proxies: ls}, nil
}
