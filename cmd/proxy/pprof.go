package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cpuguy83/go-grpc-pprof/api"
	gogotypes "github.com/gogo/protobuf/types"
)

func runPProf(sock string, args []string) {
	flags := flag.NewFlagSet("pprof", flag.ExitOnError)
	oldUsage := flags.Usage

	flags.Usage = func() {
		oldUsage()

		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "COMMANDS:")
		fmt.Fprintln(os.Stderr, "\tprofile - get a cpu profile of the running proxy daemon")
		fmt.Fprintln(os.Stderr, "\tlookup  - get dump of pprof profile (e.g. 'goroutine'")
		fmt.Fprintln(os.Stderr)
	}

	flags.Parse(args)
	args = flags.Args()

	if len(args) == 0 {
		flags.Usage()
		errorOut("Must provide a command to run")
	}

	cmd := args[0]
	var cmdArgs []string
	if len(args) > 1 {
		cmdArgs = args[1:]
	}

	switch cmd {
	case "profile":
		runPProfProfile(sock, cmdArgs)
	case "lookup":
		runPProfLookup(sock, cmdArgs)
	default:
		flags.Usage()
		errorOut(fmt.Sprintf("Unknown command: %s", cmd))
	}
}

func runPProfProfile(sock string, args []string) {
	flags := flag.NewFlagSet("profile", flag.ExitOnError)
	seconds := flags.Duration("seconds", 30*time.Second, "duration in seconds to run the profile for")
	flags.Parse(args)

	conn := makeClient(sock)
	defer conn.Close()
	client := api.NewPProfServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream, err := client.CPUProfile(ctx, &api.CPUProfileRequest{
		Duration: gogotypes.DurationProto(*seconds),
	})
	if err != nil {
		errorOut(fmt.Sprintf("error getting cpu profile: %v", err))
	}

	io.Copy(os.Stdout, api.NewChunkReader(stream))
}

func runPProfLookup(sock string, args []string) {
	flags := flag.NewFlagSet("lookup", flag.ExitOnError)
	gc := flags.Bool("gc", false, "run gc before pulling heap")
	debug := flags.Int("debug", 0, "set debug level")
	flags.Parse(args)

	args = flags.Args()
	if len(args) != 1 {
		flags.Usage()
		errorOut("Requires exactly 1 argument")
	}

	conn := makeClient(sock)
	defer conn.Close()
	client := api.NewPProfServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := client.Lookup(ctx, &api.LookupRequest{
		Name:         flags.Arg(0),
		GcBeforeHeap: *gc,
		Debug:        int32(*debug),
	})

	if err != nil {
		errorOut(fmt.Sprintf("Error during lookup request: %v", err))
	}
	io.Copy(os.Stdout, bytes.NewReader(res.Data))
}
