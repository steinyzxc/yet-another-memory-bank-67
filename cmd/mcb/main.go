package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "version":
		fmt.Println(version)
	case "healthz":
		fmt.Println("ok")
	default:
		fmt.Fprintf(os.Stderr, "unsupported command %q\n", cmd)
		os.Exit(2)
	}
}
