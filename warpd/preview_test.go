package warpd

import (
	"context"
	"strings"
	"testing"

	"github.com/pangobit/warpgate/warpd/internal/stackstate"
)

func TestPreviewServiceSeedsMixedDashboard(t *testing.T) {
	service, _, err := newPreviewService(context.Background(), "")
	if err != nil {
		t.Fatalf("newPreviewService() error = %v", err)
	}

	dashboard, err := service.Dashboard(context.Background())
	if err != nil {
		t.Fatalf("Dashboard() error = %v", err)
	}
	if !dashboard.RepositoryAttached {
		t.Fatal("preview dashboard should have an attached repository")
	}
	if dashboard.AppCount != 2 {
		t.Fatalf("AppCount = %d, want 2", dashboard.AppCount)
	}
	if dashboard.ImageUpdates != 1 {
		t.Fatalf("ImageUpdates = %d, want 1", dashboard.ImageUpdates)
	}
}

func TestPreviewMixedDeployUsesFakeDeployer(t *testing.T) {
	service, deployer, err := newPreviewService(context.Background(), PreviewScenarioMixed)
	if err != nil {
		t.Fatalf("newPreviewService() error = %v", err)
	}

	attempt, err := service.DeployStack(context.Background(), previewActor())
	if err != nil {
		t.Fatalf("DeployStack() error = %v", err)
	}
	if attempt.Status != stackstate.StatusSucceeded {
		t.Fatalf("status = %s, want %s", attempt.Status, stackstate.StatusSucceeded)
	}
	if strings.Join(deployer.deployed, ",") != "api,web" {
		t.Fatalf("deployed = %v, want api and web", deployer.deployed)
	}
}

func TestPreviewFailureScenarioExercisesFailedDeploy(t *testing.T) {
	service, deployer, err := newPreviewService(context.Background(), PreviewScenarioFailure)
	if err != nil {
		t.Fatalf("newPreviewService() error = %v", err)
	}

	attempt, err := service.DeployStack(context.Background(), previewActor())
	if err == nil {
		t.Fatal("DeployStack() error = nil, want preview failure")
	}
	if attempt.Status != stackstate.StatusRevertFailed {
		t.Fatalf("status = %s, want %s", attempt.Status, stackstate.StatusRevertFailed)
	}
	if len(deployer.deployed) == 0 || deployer.deployed[0] != "api" {
		t.Fatalf("first deployed app = %v, want api", deployer.deployed)
	}
}

func TestPreviewEmptyScenarioLeavesRepositoryDetached(t *testing.T) {
	service, _, err := newPreviewService(context.Background(), PreviewScenarioEmpty)
	if err != nil {
		t.Fatalf("newPreviewService() error = %v", err)
	}

	dashboard, err := service.Dashboard(context.Background())
	if err != nil {
		t.Fatalf("Dashboard() error = %v", err)
	}
	if dashboard.RepositoryAttached {
		t.Fatal("empty preview should not attach a repository")
	}
}

func TestPreviewRejectsUnknownScenario(t *testing.T) {
	_, _, err := newPreviewService(context.Background(), "nope")
	if err == nil {
		t.Fatal("newPreviewService() error = nil, want scenario validation error")
	}
	if !strings.Contains(err.Error(), "unknown preview scenario") {
		t.Fatalf("error = %q, want unknown scenario", err)
	}
}
