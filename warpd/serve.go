package warpd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	gliderssh "github.com/charmbracelet/ssh"
	ciapi "github.com/pangobit/warpgate/warpd/api/ci"
	sshapi "github.com/pangobit/warpgate/warpd/api/ssh"
	deployconn "github.com/pangobit/warpgate/warpd/connectors/deploy"
	githubconn "github.com/pangobit/warpgate/warpd/connectors/github"
	"github.com/pangobit/warpgate/warpd/connectors/registry"
	tursoconn "github.com/pangobit/warpgate/warpd/connectors/turso"
	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/internal/identity"
	"github.com/pangobit/warpgate/warpd/usecase"
	"github.com/sirupsen/logrus"
)

const (
	defaultConfigPollInterval = time.Minute
	defaultImagePollInterval  = 5 * time.Minute
)

// RunServe starts the Warpgate daemon: repo and image polling, the bump
// committer, the CI HTTP API, and the operator SSH TUI.
func RunServe(ctx context.Context, cfg ServeConfig) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	appConfig, err := githubconn.AppConfigFromEnv()
	if err != nil {
		return fmt.Errorf("github app credentials: %w", err)
	}
	tokens, err := githubconn.NewAppTokenProvider(appConfig)
	if err != nil {
		return fmt.Errorf("github app credentials: %w", err)
	}
	store, err := tursoconn.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := store.Close(); err != nil {
			logrus.Warnf("close store: %v", err)
		}
	}()
	service := usecase.NewService(
		store,
		githubconn.NewClientWithTokenProvider(tokens),
		registryConnector(cfg),
		deployconn.Adapter{
			SSHKey:       cfg.Deploy.SSHKey,
			TailscaleSSH: cfg.Deploy.TailscaleSSH,
			User:         cfg.Deploy.User,
			TokenSource:  tokens,
		},
	)
	actor := DaemonActor()
	if err := ensureRepositoryAttached(ctx, service, cfg.Repository, actor); err != nil {
		return err
	}

	refresh := make(chan struct{}, 1)
	scheduleRefresh := func() {
		select {
		case refresh <- struct{}{}:
		default:
		}
	}
	httpServer, err := startCIServer(ctx, service, cfg.HTTPAddr, scheduleRefresh)
	if err != nil {
		return err
	}
	sshServer, err := startTUIServer(service, cfg, scheduleRefresh)
	if err != nil {
		return err
	}

	pollErr := runPollLoop(ctx, service, actor, refresh)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return errors.Join(pollErr, httpServer.Shutdown(shutdownCtx), sshServer.Shutdown(shutdownCtx))
}

// registryConnector builds the GHCR connector. GHCR rejects GitHub App
// installation tokens, so private image reads require a dedicated registry
// token; without one the daemon authenticates anonymously and can only watch
// public images.
func registryConnector(cfg ServeConfig) *registry.GHCR {
	if cfg.RegistryToken != "" {
		return registry.NewGHCRWithTokenProvider(registry.StaticToken(cfg.RegistryToken))
	}
	logrus.Warn("WARPGATE_REGISTRY_TOKEN is not set; private GHCR images cannot be watched (GitHub App tokens are not accepted by GHCR)")
	return registry.NewGHCR()
}

// DaemonActor is the identity used for daemon-initiated operations.
func DaemonActor() identity.User {
	return identity.User{
		Email:        "daemon@warpgate",
		DisplayName:  "Warpgate Daemon",
		Capabilities: []string{identity.AdminCapability},
	}
}

// CIActor is the identity granted to callers of the CI HTTP API.
func CIActor() identity.User {
	return identity.User{
		Email:        "ci@warpgate",
		DisplayName:  "Warpgate CI",
		Capabilities: []string{identity.AdminCapability},
	}
}

// ensureRepositoryAttached attaches the configured repository when it differs
// from the stored attachment, and fails when no repository is available.
func ensureRepositoryAttached(ctx context.Context, service *usecase.Service, repo RepoConfig, actor identity.User) error {
	settings, attached, err := service.RepositorySettings(ctx)
	if err != nil {
		return err
	}
	if repo.Owner == "" || repo.Repo == "" {
		if !attached {
			return errors.New("no repository attached: set WARPGATE_REPO=owner/repo")
		}
		return nil
	}
	if attached && settings.Owner == repo.Owner && settings.Repo == repo.Repo && settings.Branch == repo.Branch && settings.Path == repo.Path {
		return nil
	}
	err = service.AttachRepository(ctx, actor, configrepo.RepositorySettings{
		Owner:  repo.Owner,
		Repo:   repo.Repo,
		Branch: repo.Branch,
		Path:   repo.Path,
	})
	if err != nil && repo.Path == "" && strings.Contains(err.Error(), "cluster.yml") {
		return fmt.Errorf("%w (if cluster.yml lives in a repository subdirectory, set WARPGATE_REPO_PATH, e.g. WARPGATE_REPO_PATH=prod)", err)
	}
	return err
}

func startCIServer(ctx context.Context, service *usecase.Service, addr string, refresh func()) (*http.Server, error) {
	router := ciapi.NewRouter(service, identity.StaticIdentifier{User: CIActor()}, refresh)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen for CI API: %w", err)
	}
	server := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logrus.Errorf("CI API server: %v", err)
		}
	}()
	logrus.Infof("CI API listening on %s", listener.Addr())
	return server, nil
}

func startTUIServer(service *usecase.Service, cfg ServeConfig, refresh func()) (*gliderssh.Server, error) {
	server, err := sshapi.NewServer(service, sshapi.Config{
		Addr:        cfg.SSHAddr,
		HostKeyPath: cfg.HostKeyPath,
		Refresh:     refresh,
	})
	if err != nil {
		return nil, fmt.Errorf("create TUI SSH server: %w", err)
	}
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, gliderssh.ErrServerClosed) {
			logrus.Errorf("TUI SSH server: %v", err)
		}
	}()
	logrus.Infof("operator TUI listening on ssh://%s", cfg.SSHAddr)
	return server, nil
}

// runPollLoop serializes daemon reconciliation: sync config, check images,
// and commit pending bumps on each tick or CI refresh nudge.
func runPollLoop(ctx context.Context, service *usecase.Service, actor identity.User, refresh <-chan struct{}) error {
	configInterval, imageInterval := pollIntervals(ctx, service)
	configTicker := time.NewTicker(configInterval)
	defer configTicker.Stop()
	imageTicker := time.NewTicker(imageInterval)
	defer imageTicker.Stop()
	reconcile(ctx, service, actor, true)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-configTicker.C:
			reconcile(ctx, service, actor, false)
		case <-imageTicker.C:
			reconcile(ctx, service, actor, true)
		case <-refresh:
			reconcile(ctx, service, actor, true)
		}
	}
}

func pollIntervals(ctx context.Context, service *usecase.Service) (time.Duration, time.Duration) {
	configInterval := defaultConfigPollInterval
	imageInterval := defaultImagePollInterval
	settings, err := service.PollerSettings(ctx)
	if err != nil {
		logrus.Warnf("load poller settings: %v", err)
		return configInterval, imageInterval
	}
	if settings.ConfigInterval > 0 {
		configInterval = settings.ConfigInterval
	}
	if settings.ImagesInterval > 0 {
		imageInterval = settings.ImagesInterval
	}
	return configInterval, imageInterval
}

func reconcile(ctx context.Context, service *usecase.Service, actor identity.User, includeImages bool) {
	if err := service.SyncConfig(ctx, actor); err != nil {
		logrus.Errorf("sync config: %v", err)
	}
	if !includeImages {
		return
	}
	if err := service.CheckImages(ctx, actor); err != nil {
		logrus.Errorf("check images: %v", err)
	}
	records, err := service.CommitImageBumps(ctx, actor)
	if err != nil {
		logrus.Errorf("commit image bumps: %v", err)
	}
	for _, record := range records {
		logrus.Infof("committed bump release %s for %s at %s", record.ID, record.App, record.ConfigCommit)
	}
}
