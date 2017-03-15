package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()

		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "COMMANDS:")
		fmt.Fprintln(os.Stderr, "\tserve - run the proxy daemon")
		fmt.Fprintln(os.Stderr, "\tdebug - run various debug commands")
		fmt.Fprintln(os.Stderr)
	}

	flag.Parse()
	runCmd()
}

func runCmd() {
	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		fmt.Fprintln(os.Stderr, "Must provide a command to run")
		return
	}

	cmd := args[0]
	var cmdArgs []string
	if len(args) > 1 {
		cmdArgs = args[1:]
	}

	switch cmd {
	case "debug":
		runDebug(cmdArgs)
	case "serve":
		runSrv(cmdArgs)
	default:
		errorOut(fmt.Sprintf("Unknown command: %s", cmd))
		flag.Usage()
	}
}
