// Package main is the warpd daemon entrypoint.
package main

import (
	"fmt"
	"os"

	"github.com/pangobit/warpgate/pkg/daemon"
)

func main() {
	if err := daemon.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
