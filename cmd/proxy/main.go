package main

import (
	"flag"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	"github.com/Sirupsen/logrus"
	"github.com/docker/libnetwork/cmd/proxy/rpc"
)

func main() {
	logLevel := flag.String("log-level", "info", "set the log level for logging output")
	flag.Parse()

	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logrus.Fatal(err)
	}
	logrus.SetLevel(level)

	l, err := net.Listen("unix", os.Args[1])
	if err != nil {
		logrus.Fatalf("error starting grpc listener: %v", err)
	}

	srv := &server{
		rpc: grpc.NewServer(grpc.UnaryInterceptor(middleware)),
	}

	rpc.RegisterProxyServer(srv.rpc, srv)
	go handleStopSignals(srv)

	srv.Serve(l)
}

func handleStopSignals(srv *server) {
	s := make(chan os.Signal, 10)
	signal.Notify(s, os.Interrupt, syscall.SIGTERM)

	for range s {
		srv.shutdown()
	}
	os.Exit(0)
}
