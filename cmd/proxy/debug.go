package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/docker/libnetwork/api/proxy"

	"google.golang.org/grpc"
)

func runDebug(args []string) {
	flags := flag.NewFlagSet("debug", flag.ContinueOnError)
	sock := flags.String("sock", "", "location of unix socket to connect to proxy service")

	oldUsage := flags.Usage

	flags.Usage = func() {
		oldUsage()

		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "COMMANDS:")
		fmt.Fprintln(os.Stderr, "\tstart - start proxying on a new port")
		fmt.Fprintln(os.Stderr, "\tstop  - stop proxying a port")
		fmt.Fprintln(os.Stderr, "\tls    - list all running proxies")
		fmt.Fprintln(os.Stderr, "\tpprof - generate pprof profiles from the proxy daemon")
		fmt.Fprintln(os.Stderr)
	}

	flags.Parse(args)
	if *sock == "" {
		flags.Usage()
		errorOut("Must provide the sock flag")
	}

	args = flags.Args()
	if len(args) == 0 {
		errorOut("Must provide a debug command to run")
	}
	cmd := args[0]

	if len(args) > 1 {
		args = args[1:]
	} else {
		args = nil
	}

	switch cmd {
	case "ls":
		runList(*sock, args)
	case "start":
		runStart(*sock, args)
	case "stop":
		runStop(*sock, args)
	case "pprof":
		runPProf(*sock, args)
	default:
		flags.Usage()
		errorOut(fmt.Sprintf("Unknown command: %s", cmd))
	}
}

func makeClient(sock string) *grpc.ClientConn {
	c, err := grpc.Dial(sock, grpc.WithBlock(), grpc.WithTimeout(10*time.Second), grpc.WithInsecure(), grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout("unix", addr, timeout)
	}))

	if err != nil {
		errorOut(fmt.Sprintf("Error connecting to proxy service: %v", err))
	}
	return c
}

func errorOut(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

func runList(sock string, args []string) {
	conn := makeClient(sock)
	defer conn.Close()
	client := proxy.NewProxyClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ls, err := client.List(ctx, nil)
	if err != nil {
		errorOut(fmt.Sprintf("Error getting proxy list: %v", err))
	}

	buf := bytes.NewBuffer(nil)
	tw := tabwriter.NewWriter(buf, 1, 8, 1, '\t', 0)
	fmt.Fprintf(tw, "FRONTEND\tBACKEND\n")
	for _, p := range ls.Proxies {
		backend := "Disabled"
		if p.Backend != nil {
			backend = net.JoinHostPort(p.Backend.Addr, strconv.Itoa(int(p.Backend.Port)))
		}
		fmt.Fprintf(tw, "%s\t%s\n", proxy.Key(p), backend)
	}
	tw.Flush()
	io.Copy(os.Stdout, buf)
}

func runStart(sock string, args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	frontend := fs.String("frontend", "", "frontend <ip>:<port> to proxy traffic from")
	backend := fs.String("backend", "", "backend <ip>:<port> to proxy traffic to")
	proto := fs.String("proto", "tcp", "protocol to proxy with (tcp or udp)")
	fs.Parse(args)

	switch *proto {
	case "udp", "tcp":
	default:
		errorOut(fmt.Sprintf("Unsupported protocol: %s", proto))
	}

	fa, fp, fpp, err := splitAddrString(*frontend)
	if err != nil {
		errorOut(fmt.Sprintf("Error reading frontend spec %q: %v", *frontend, err))
	}

	if fpp != "" {
		errorOut(fmt.Sprintf("Invalid frontend format, must not specify proto can only be set as a flag: %s", *frontend))
	}

	ba, bp, bpp, err := splitAddrString(*backend)
	if err != nil {
		if *backend != "" {
			errorOut(fmt.Sprintf("error reading backend spec %q: %v", *backend, err))
		}
	}
	if bpp != "" {
		errorOut(fmt.Sprintf("Invalid backend format, must not specify proto can only be set as a flag: %s", *backend))
	}

	conn := makeClient(sock)
	defer conn.Close()
	client := proxy.NewProxyClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &proxy.StartProxyRequest{
		Spec: &proxy.ProxySpec{
			Protocol: *proto,
			Frontend: &proxy.ProxySpec_EndpointSpec{
				Addr: fa,
				Port: uint32(fp),
			},
		},
	}
	if ba != "" {
		req.Spec.Backend = &proxy.ProxySpec_EndpointSpec{
			Addr: ba,
			Port: uint32(bp),
		}
	}
	_, err = client.StartProxy(ctx, req)
	if err != nil {
		errorOut(fmt.Sprintf("Error starting proxy: %v", err))
	}
}

func splitAddrString(s string) (addr string, port int, proto string, err error) {
	addr, portSpec, err := net.SplitHostPort(s)
	if err != nil {
		return
	}

	split := strings.Split(portSpec, "/")
	if len(split) > 1 {
		proto = split[1]
	}
	port, err = strconv.Atoi(split[0])
	return
}

func runStop(sock string, args []string) {
	conn := makeClient(sock)
	defer conn.Close()
	client := proxy.NewProxyClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var wg sync.WaitGroup

	for _, arg := range args {
		addr, port, proto, err := splitAddrString(arg)
		if err != nil {
			fmt.Fprintln(os.Stderr, fmt.Sprintf("Error parsing host spec %q: %v", arg, err))
		}
		wg.Add(1)
		go func(arg string) {
			defer wg.Done()
			req := &proxy.StopProxyRequest{
				Spec: &proxy.ProxySpec{
					Protocol: proto,
					Frontend: &proxy.ProxySpec_EndpointSpec{
						Addr: addr,
						Port: uint32(port),
					},
				},
			}
			_, err := client.StopProxy(ctx, req)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error stopping proxy %s: %v\n", arg, err)
				return
			}
			fmt.Println(req.Spec)
			fmt.Fprintln(os.Stdout, arg)
		}(arg)
	}
	wg.Wait()
}
