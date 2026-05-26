// Package main is the warpd daemon entrypoint.
package main

import (
	"fmt"
	"os"

	"github.com/pangobit/warpgate/warpd"
)

func main() {
	if err := warpd.Main(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
