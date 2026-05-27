package warpd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	httpapi "github.com/pangobit/warpgate/warpd/api/http"
	webapi "github.com/pangobit/warpgate/warpd/api/web"
	deployconn "github.com/pangobit/warpgate/warpd/connectors/deploy"
	githubconn "github.com/pangobit/warpgate/warpd/connectors/github"
	"github.com/pangobit/warpgate/warpd/connectors/registry"
	tursoconn "github.com/pangobit/warpgate/warpd/connectors/turso"
	"github.com/pangobit/warpgate/warpd/usecase"
	"github.com/sirupsen/logrus"
)

// RunLocalUI starts the local browser UI.
func RunLocalUI(ctx context.Context, cfg LocalUIConfig) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg = localUIDefaults(cfg)
	store, err := tursoconn.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := store.Close(); err != nil {
			logrus.Warnf("close store: %v", err)
		}
	}()
	githubAuth, err := loadGitHubSession(ctx, cfg.GitHubClientID, store)
	if err != nil {
		return err
	}
	service := newLocalService(store, githubAuth, cfg.Deploy)
	assets := webapi.NewAssets()
	router := httpapi.NewRouter(service, githubAuth, assets, httpapi.WithGitHubAuth(githubAuth))
	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen for local UI: %w", err)
	}
	server := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return serveLocalUI(ctx, server, listener, cfg.OpenBrowser)
}

func localUIDefaults(cfg LocalUIConfig) LocalUIConfig {
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = "127.0.0.1:0"
	}
	if cfg.DBPath == "" {
		cfg.DBPath = defaultLocalDBPath()
	}
	return cfg
}

func loadGitHubSession(ctx context.Context, clientID string, store *tursoconn.Store) (*githubconn.DeviceSession, error) {
	githubAuth := githubconn.NewDeviceSession(clientID, store)
	if err := githubAuth.Load(ctx); err != nil {
		return nil, fmt.Errorf("load GitHub session: %w", err)
	}
	return githubAuth, nil
}

func newLocalService(store *tursoconn.Store, githubAuth *githubconn.DeviceSession, deploy DeployConfig) *usecase.Service {
	return usecase.NewService(
		store,
		githubconn.NewClientWithTokenProvider(githubAuth),
		registry.NewGHCR(),
		deployconn.Adapter{
			RepoPath:          deploy.RepoPath,
			SSHKey:            deploy.SSHKey,
			TailscaleSSH:      deploy.TailscaleSSH,
			User:              deploy.User,
			GitHubTokenEnvVar: deploy.GitHubTokenEnvVar,
		},
	)
}

func serveLocalUI(ctx context.Context, server *http.Server, listener net.Listener, open bool) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()
	url := localURL(listener.Addr())
	if _, err := fmt.Fprintf(os.Stdout, "Warpgate UI: %s\n", url); err != nil {
		return err
	}
	if open {
		if err := openBrowser(ctx, url); err != nil {
			logrus.Warnf("open browser: %v", err)
		}
	}
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

func localURL(addr net.Addr) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "http://" + addr.String() + "/"
	}
	if host == "" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/"
}

func openBrowser(ctx context.Context, target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", target)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", target)
	}
	return cmd.Start()
}
