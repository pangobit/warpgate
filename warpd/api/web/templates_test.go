package web

import (
	"bytes"
	"strings"
	"testing"
)

func TestDashboardRendersIdentityInNavigation(t *testing.T) {
	var body bytes.Buffer
	if err := NewRenderer().Render(&body, Dashboard(DashboardPage{
		Title: "Warpgate",
	})); err != nil {
		t.Fatalf("render dashboard: %v", err)
	}

	if !strings.Contains(body.String(), ">unknown<") {
		t.Fatalf("expected navigation identity label in dashboard HTML")
	}
}

func TestIdentityAuthStyleUsesStatusColors(t *testing.T) {
	if identityAuthStyle("unknown").ClassName() != statusWarning().ClassName() {
		t.Fatalf("expected unknown identity to use warning status")
	}
	if identityAuthStyle("ray@example.com").ClassName() != statusSuccess().ClassName() {
		t.Fatalf("expected known identity to use success status")
	}
}
