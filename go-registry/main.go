package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "server":
		err = commandServer(os.Args[2:])
	case "token":
		err = commandToken(os.Args[2:])
	case "node":
		err = commandNode(os.Args[2:])
	case "-h", "--help", "help":
		usage(os.Stdout)
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  vpn-registry server -config ./config.json")
	fmt.Fprintln(w, "  vpn-registry token list -config ./config.json")
	fmt.Fprintln(w, "  vpn-registry token create -config ./config.json")
	fmt.Fprintln(w, "  vpn-registry token delete -config ./config.json -token xxx")
	fmt.Fprintln(w, "  vpn-registry node list -config ./config.json")
	fmt.Fprintln(w, "  vpn-registry node show -config ./config.json -id us-la-001")
	fmt.Fprintln(w, "  vpn-registry node delete -config ./config.json -id us-la-001")
}
