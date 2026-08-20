package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/doctor"
)

func newTestStatusRuntimeProbeCheck(outcome statusRuntimeProbeOutcome, err error) *statusRuntimeProbeCheck {
	check := newStatusRuntimeProbeCheck("/tmp/city", &config.City{Workspace: config.Workspace{Name: "demo"}})
	check.probe = func() (statusRuntimeProbeOutcome, error) { return outcome, err }
	return check
}

func TestStatusRuntimeProbeCheckIdentity(t *testing.T) {
	check := newStatusRuntimeProbeCheck("/tmp/city", nil)
	if got := check.Name(); got != "status-runtime-probe" {
		t.Fatalf("check name = %q, want status-runtime-probe", got)
	}
	if check.CanFix() {
		t.Fatal("CanFix = true; the probe is diagnostic-only")
	}
	if err := check.Fix(nil); err != nil {
		t.Fatalf("Fix returned %v, want nil for a diagnostic-only check", err)
	}
	if check.WarmupEligible() {
		t.Fatal("WarmupEligible = true; the probe forks a runtime subprocess and must stay out of gc start")
	}
}

func TestStatusRuntimeProbeCheckPassesWithHeadroom(t *testing.T) {
	check := newTestStatusRuntimeProbeCheck(statusRuntimeProbeOutcome{
		SessionName: "demo__mayor",
		Elapsed:     statusProviderCallTimeout / 10,
	}, nil)

	got := check.Run(&doctor.CheckContext{})
	if got.Status != doctor.StatusOK {
		t.Fatalf("status = %v (%q), want StatusOK for a probe well inside the bound", got.Status, got.Message)
	}
}

func TestStatusRuntimeProbeCheckReportsTimedOutProbe(t *testing.T) {
	check := newTestStatusRuntimeProbeCheck(statusRuntimeProbeOutcome{
		SessionName: "demo__mayor",
		Elapsed:     statusProviderCallTimeout,
		Partial:     true,
	}, nil)

	got := check.Run(&doctor.CheckContext{})
	if got.Status != doctor.StatusError {
		t.Fatalf("status = %v (%q), want StatusError when the probe trips the bound", got.Status, got.Message)
	}
	if got.Severity != doctor.SeverityAdvisory {
		t.Fatalf("severity = %v, want SeverityAdvisory; a blind gc status degrades observability without stopping work", got.Severity)
	}
	if !strings.Contains(got.Message, statusProviderCallTimeout.String()) {
		t.Fatalf("message = %q, want the bound %s named so the operator can size it", got.Message, statusProviderCallTimeout)
	}
}

func TestStatusRuntimeProbeCheckWarnsOnThinHeadroom(t *testing.T) {
	elapsed := statusProviderCallTimeout/statusRuntimeProbeHeadroomDivisor + time.Millisecond
	check := newTestStatusRuntimeProbeCheck(statusRuntimeProbeOutcome{
		SessionName: "demo__mayor",
		Elapsed:     elapsed,
	}, nil)

	got := check.Run(&doctor.CheckContext{})
	if got.Status != doctor.StatusWarning {
		t.Fatalf("status = %v (%q), want StatusWarning for a probe past 1/%d of the bound", got.Status, got.Message, statusRuntimeProbeHeadroomDivisor)
	}
	if got.Severity != doctor.SeverityAdvisory {
		t.Fatalf("severity = %v, want SeverityAdvisory", got.Severity)
	}
}

func TestStatusRuntimeProbeCheckWarnsWhenProbeCannotRun(t *testing.T) {
	check := newTestStatusRuntimeProbeCheck(statusRuntimeProbeOutcome{}, errors.New("no session provider"))

	got := check.Run(&doctor.CheckContext{})
	if got.Status != doctor.StatusWarning {
		t.Fatalf("status = %v (%q), want StatusWarning when the runtime cannot be probed at all", got.Status, got.Message)
	}
	if got.Severity != doctor.SeverityAdvisory {
		t.Fatalf("severity = %v, want SeverityAdvisory", got.Severity)
	}
	if !strings.Contains(got.Message, "no session provider") {
		t.Fatalf("message = %q, want the construction error surfaced", got.Message)
	}
}

func TestStatusRuntimeProbeSessionNameUsesConfiguredAgent(t *testing.T) {
	cfg := &config.City{
		Workspace: config.Workspace{Name: "demo"},
		Agents:    []config.Agent{{Name: "mayor"}, {Name: "witness"}},
	}
	if got := statusRuntimeProbeSessionName("/tmp/demo", cfg); got != "mayor" {
		t.Fatalf("probe session name = %q, want the first configured agent's runtime session name %q", got, "mayor")
	}
}

func TestStatusRuntimeProbeSessionNameFallsBackWithoutAgents(t *testing.T) {
	cfg := &config.City{Workspace: config.Workspace{Name: "demo"}}
	if got := statusRuntimeProbeSessionName("/tmp/demo", cfg); got != statusRuntimeProbeFallbackSession {
		t.Fatalf("probe session name = %q, want fallback %q when no agents are configured", got, statusRuntimeProbeFallbackSession)
	}
}

func TestBuildDoctorChecksRegistersStatusRuntimeProbe(t *testing.T) {
	cityDir := t.TempDir()
	t.Setenv("GC_DOLT", "skip")
	cfg := &config.City{Workspace: config.Workspace{Name: "demo"}}

	names := doctorCheckNames(buildDoctorChecks(cityDir, cfg, nil, buildDoctorChecksOpts{
		SkipCityDoltCheck:    true,
		SkipManagedDoltCheck: true,
	}))
	if doctorCheckIndex(names, "status-runtime-probe") < 0 {
		t.Fatalf("status-runtime-probe check missing: %v", names)
	}
}
