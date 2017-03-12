package main

import (
	"errors"
	"net"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/docker/libnetwork/cmd/proxy/rpc"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

type server struct {
	mu      sync.Mutex
	rpc     *grpc.Server
	proxies map[string]Proxy
}

func middleware(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	logrus.WithField("request", req).Infof("begin %s", info.FullMethod)
	resp, err = handler(ctx, req)
	logrus.WithField("request", req).Infof("end %s", info.FullMethod)
	return resp, err
}

func (s *server) shutdown() {
	s.mu.Lock()
	s.rpc.GracefulStop()

	for _, p := range s.proxies {
		p.Close()
	}

	s.mu.Unlock()
}

func (s *server) Serve(l net.Listener) error {
	s.proxies = make(map[string]Proxy)
	return s.rpc.Serve(l)
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

	if p, exists := s.proxies[frontend.String()]; exists {
		if p.BackendAddr().String() == backend.String() {
			return nil, grpc.Errorf(codes.AlreadyExists, "proxy for %q --> %q already exists", frontend.String(), backend.String())
		}
		return nil, grpc.Errorf(codes.FailedPrecondition, "proxy for %q already exists", frontend.String())
	}

	p, err := NewProxy(frontend, backend)
	if err != nil {
		return nil, err
	}
	s.proxies[frontend.String()] = p
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
	frontend, _, err := getProxyAddrs(req.Spec)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	p := s.proxies[frontend.String()]
	if p != nil {
		p.Close()
		delete(s.proxies, frontend.String())
	}

	return &rpc.StopProxyResponse{}, nil
}
