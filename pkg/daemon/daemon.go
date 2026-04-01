// Package daemon implements the warpd server and agent modes.
package daemon

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"
)

// Run starts the warpgate daemon in server or agent mode based on WARPGATE_MODE.
func Run() error {
	mode := os.Getenv("WARPGATE_MODE")
	if mode == "" {
		mode = "server"
	}

	logrus.Infof("Starting Warpgate daemon in %s mode", mode)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	switch mode {
	case "server":
		return runServer(ctx, sigChan)
	case "agent":
		return runAgent(ctx, sigChan)
	default:
		return fmt.Errorf("unknown mode: %s (must be 'server' or 'agent')", mode)
	}
}

func runServer(ctx context.Context, sigChan chan os.Signal) error {
	logrus.Info("Starting Warpgate control plane...")

	// TODO: Initialize API server
	// TODO: Initialize file watcher
	// TODO: Initialize deployment orchestrator

	select {
	case <-ctx.Done():
		return ctx.Err()
	case sig := <-sigChan:
		logrus.Infof("Received signal %s, shutting down...", sig)
		return nil
	}
}

func runAgent(ctx context.Context, sigChan chan os.Signal) error {
	logrus.Info("Starting Warpgate node agent...")

	// TODO: Connect to control plane
	// TODO: Start local Docker Compose management
	// TODO: Report node status

	select {
	case <-ctx.Done():
		return ctx.Err()
	case sig := <-sigChan:
		logrus.Infof("Received signal %s, shutting down...", sig)
		return nil
	}
}
