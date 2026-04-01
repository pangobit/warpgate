// Package main is the warpgate CLI entrypoint.
package main

import (
	"fmt"
	"os"

	"github.com/pangobit/warpgate/pkg/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
