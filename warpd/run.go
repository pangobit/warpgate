package warpd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpapi "github.com/pangobit/warpgate/warpd/api/http"
	webapi "github.com/pangobit/warpgate/warpd/api/web"
	deployconn "github.com/pangobit/warpgate/warpd/connectors/deploy"
	githubconn "github.com/pangobit/warpgate/warpd/connectors/github"
	"github.com/pangobit/warpgate/warpd/connectors/registry"
	tailscaleconn "github.com/pangobit/warpgate/warpd/connectors/tailscale"
	tursoconn "github.com/pangobit/warpgate/warpd/connectors/turso"
	"github.com/pangobit/warpgate/warpd/internal/identity"
	"github.com/pangobit/warpgate/warpd/usecase"
	"github.com/sirupsen/logrus"
)

// Run loads configuration and starts warpd.
func Run(args []string) error {
	cfg, err := LoadConfig(args)
	if err != nil {
		return err
	}
	switch cfg.Mode {
	case "server":
		return RunServer(context.Background(), cfg)
	case "agent":
		return fmt.Errorf("agent mode is reserved for a later Warpgate 2.x release")
	default:
		return fmt.Errorf("unknown mode %q", cfg.Mode)
	}
}

// RunServer starts the Warpgate web control plane.
func RunServer(ctx context.Context, cfg Config) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := tursoconn.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()

	identifier := identifierForConfig(cfg)

	service := usecase.NewService(
		store,
		githubconn.NewClient(),
		registry.NewGHCR(),
		deployconn.Adapter{
			RepoPath:          cfg.Deploy.RepoPath,
			SSHKey:            cfg.Deploy.SSHKey,
			TailscaleSSH:      cfg.Deploy.TailscaleSSH,
			User:              cfg.Deploy.User,
			GitHubTokenEnvVar: cfg.Deploy.GitHubTokenEnvVar,
		},
	)
	stopPollers := startPollers(ctx, service)
	defer stopPollers()
	assets := webapi.NewAssets()
	router := httpapi.NewRouter(service, identifier, assets)
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		logrus.Infof("warpd listening on %s", cfg.HTTPAddr)
		errCh <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func startPollers(ctx context.Context, service *usecase.Service) func() {
	ctx, cancel := context.WithCancel(ctx)
	go runPoller(ctx, service, "config")
	go runPoller(ctx, service, "images")
	return cancel
}

func identifierForConfig(cfg Config) identity.Identifier {
	if cfg.LocalDev || cfg.AuthMode == "static" {
		return identity.StaticIdentifier{User: identity.User{
			Email:        cfg.LocalEmail,
			DisplayName:  cfg.LocalEmail,
			Capabilities: []string{identity.AdminCapability},
		}}
	}
	if cfg.AuthMode == "tailscale" {
		return tailscaleconn.NewIdentifier()
	}
	return identity.StaticIdentifier{User: identity.User{
		Email:        "unknown",
		DisplayName:  "unknown",
		Capabilities: []string{identity.AdminCapability},
	}}
}

func runPoller(ctx context.Context, service *usecase.Service, name string) {
	settings, err := service.PollerSettings(ctx)
	if err != nil {
		logrus.Warnf("poller settings unavailable: %v", err)
		return
	}
	enabled := settings.ConfigEnabled
	interval := settings.ConfigInterval
	if name == "images" {
		enabled = settings.ImagesEnabled
		interval = settings.ImagesInterval
	}
	if !enabled {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	automation := identity.User{Email: "warpd@localhost", DisplayName: "warpd"}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var err error
			if name == "images" {
				err = service.CheckImages(ctx, automation)
			} else {
				err = service.SyncConfig(ctx, automation)
			}
			if err != nil {
				logrus.Warnf("%s poll failed: %v", name, err)
			}
		}
	}
}

// Main runs the daemon with process arguments.
func Main() error {
	return Run(os.Args[1:])
}
