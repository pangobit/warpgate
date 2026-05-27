package http

import (
	webapi "github.com/pangobit/warpgate/warpd/api/web"
	"github.com/pangobit/warpgate/warpd/internal/configrepo"
	"github.com/pangobit/warpgate/warpd/internal/deployment"
	"github.com/pangobit/warpgate/warpd/internal/identity"
	"github.com/pangobit/warpgate/warpd/internal/release"
	"github.com/pangobit/warpgate/warpd/usecase"
)

func dashboardView(dashboard usecase.Dashboard) webapi.DashboardView {
	return webapi.DashboardView{
		RepositoryAttached: dashboard.RepositoryAttached,
		Repository:         repositoryView(dashboard.Repository),
		ConfigCursor: webapi.SyncCursorView{
			LastObservedCommit: dashboard.ConfigCursor.LastObservedCommit,
			LastCheckedAt:      dashboard.ConfigCursor.LastCheckedAt,
			LastError:          dashboard.ConfigCursor.LastError,
		},
		AppCount:     dashboard.AppCount,
		ImageUpdates: dashboard.ImageUpdates,
	}
}

func repositoryView(settings configrepo.RepositorySettings) webapi.RepositoryView {
	return webapi.RepositoryView{
		Owner:  settings.Owner,
		Repo:   settings.Repo,
		Branch: settings.Branch,
		Path:   settings.Path,
	}
}

func appViews(apps []configrepo.AppSnapshot) []webapi.AppView {
	views := make([]webapi.AppView, 0, len(apps))
	for _, app := range apps {
		views = append(views, appView(app))
	}
	return views
}

func appView(app configrepo.AppSnapshot) webapi.AppView {
	return webapi.AppView{
		Name:         app.Name,
		Path:         app.Path,
		ConfigCommit: app.ConfigCommit,
		RawYAML:      app.RawYAML,
	}
}

func runtimeStatusView(status usecase.RuntimeStatus) webapi.RuntimeStatusView {
	return webapi.RuntimeStatusView{
		Nodes: runtimeNodeViews(status.Nodes),
		Apps:  runtimeAppStatusViews(status.Apps),
	}
}

func runtimeNodeViews(nodes []usecase.RuntimeNode) []webapi.RuntimeNodeView {
	views := make([]webapi.RuntimeNodeView, 0, len(nodes))
	for _, node := range nodes {
		views = append(views, webapi.RuntimeNodeView{
			ID:        node.ID,
			Host:      node.Host,
			PrivateIP: node.PrivateIP,
			Reachable: node.Reachable,
		})
	}
	return views
}

func runtimeAppStatusViews(apps []usecase.RuntimeAppStatus) []webapi.RuntimeAppStatusView {
	views := make([]webapi.RuntimeAppStatusView, 0, len(apps))
	for _, app := range apps {
		views = append(views, webapi.RuntimeAppStatusView{
			App:           app.App,
			NodeID:        app.NodeID,
			Version:       app.Version,
			Slot:          app.Slot,
			State:         app.State,
			Services:      runtimeContainerStatusViews(app.Services),
			Error:         app.Error,
			ShadowVersion: app.ShadowVersion,
			ShadowState:   app.ShadowState,
		})
	}
	return views
}

func runtimeContainerStatusViews(services []usecase.RuntimeContainerStatus) []webapi.RuntimeContainerStatusView {
	views := make([]webapi.RuntimeContainerStatusView, 0, len(services))
	for _, service := range services {
		views = append(views, webapi.RuntimeContainerStatusView{
			Service: service.Service,
			Name:    service.Name,
			State:   service.State,
		})
	}
	return views
}

func configNodeViews(nodes []usecase.ConfigNode) []webapi.ConfigNodeView {
	views := make([]webapi.ConfigNodeView, 0, len(nodes))
	for _, node := range nodes {
		views = append(views, webapi.ConfigNodeView{
			ID:        node.ID,
			Host:      node.Host,
			PrivateIP: node.PrivateIP,
		})
	}
	return views
}

func logsRequestView(input usecase.LogsInput) webapi.LogsRequestView {
	return webapi.LogsRequestView{
		NodeID: input.NodeID,
		App:    input.App,
		Tail:   input.Tail,
		Grep:   input.Grep,
	}
}

func logsResultView(result usecase.LogsResult) webapi.LogsResultView {
	return webapi.LogsResultView{
		Output:  result.Output,
		Message: result.Message,
	}
}

func appDetailView(detail usecase.AppDetail) webapi.AppDetailView {
	return webapi.AppDetailView{
		App:         appView(detail.App),
		Services:    appReleaseServiceViews(detail.Services),
		Releases:    releaseViews(detail.Releases),
		Deployments: deploymentViews(detail.Deployments),
	}
}

func appReleaseServiceViews(services []usecase.AppReleaseService) []webapi.AppReleaseServiceView {
	views := make([]webapi.AppReleaseServiceView, 0, len(services))
	for _, service := range services {
		views = append(views, webapi.AppReleaseServiceView{
			Name:        service.Name,
			Image:       service.Image,
			ImageTag:    service.ImageTag,
			ImageDigest: service.ImageDigest,
		})
	}
	return views
}

func releaseViews(records []release.Record) []webapi.ReleaseView {
	views := make([]webapi.ReleaseView, 0, len(records))
	for _, record := range records {
		views = append(views, releaseView(record))
	}
	return views
}

func releaseView(record release.Record) webapi.ReleaseView {
	return webapi.ReleaseView{
		ID:           record.ID,
		App:          record.App,
		ConfigCommit: record.ConfigCommit,
		ManifestJSON: record.ManifestJSON,
		RawYAML:      record.RawYAML,
		Status:       string(record.Status),
		ActorEmail:   record.ActorEmail,
		CreatedAt:    record.CreatedAt,
	}
}

func deploymentViews(records []deployment.Record) []webapi.DeploymentView {
	views := make([]webapi.DeploymentView, 0, len(records))
	for _, record := range records {
		views = append(views, webapi.DeploymentView{
			ID:           record.ID,
			ReleaseID:    record.ReleaseID,
			App:          record.App,
			Targets:      record.Targets,
			ActorEmail:   record.ActorEmail,
			Status:       string(record.Status),
			StartedAt:    record.StartedAt,
			FinishedAt:   record.FinishedAt,
			ErrorMessage: record.ErrorMessage,
		})
	}
	return views
}

func githubAuthView(status identity.GitHubAuthStatus) webapi.GitHubAuthView {
	return webapi.GitHubAuthView{
		Configured:      status.Configured,
		ClientID:        status.ClientID,
		Authenticated:   status.Authenticated,
		Login:           status.Login,
		DisplayName:     status.DisplayName,
		UserCode:        status.UserCode,
		VerificationURI: status.VerificationURI,
		Error:           status.Error,
	}
}
