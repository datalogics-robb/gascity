package main

import (
	"fmt"
	"time"

	"github.com/gastownhall/gascity/internal/agent"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/doctor"
	"github.com/gastownhall/gascity/internal/runtime"
)

// statusRuntimeProbeHeadroomDivisor sets how much of statusProviderCallTimeout
// a healthy probe may consume before the check warns. A probe past half the
// bound is one loaded host away from tripping it.
const statusRuntimeProbeHeadroomDivisor = 2

// statusRuntimeProbeFallbackSession names the probe target for a city with no
// agents configured. Any name exercises the same runtime round-trip, because a
// provider answers "is this session live" out of one bulk state read.
const statusRuntimeProbeFallbackSession = "gc-doctor-status-probe"

// statusRuntimeProbeCheck runs one bounded runtime status probe — the same call
// gc status and the API /status projection fan out per agent — against the
// city's real runtime, and reports whether it answered inside
// statusProviderCallTimeout. A probe that trips that bound makes both
// projections report every non-running agent as "unknown (partial status)"
// while every other doctor check still passes.
type statusRuntimeProbeCheck struct {
	cityPath string
	cfg      *config.City
	probe    func() (statusRuntimeProbeOutcome, error)
}

// statusRuntimeProbeOutcome records the result of one runtime status probe.
type statusRuntimeProbeOutcome struct {
	// SessionName is the runtime session name the probe named.
	SessionName string
	// Elapsed is how long the bounded probe took to answer.
	Elapsed time.Duration
	// Partial is true when the probe tripped statusProviderCallTimeout.
	Partial bool
}

// newStatusRuntimeProbeCheck builds the status-runtime-probe doctor check. The
// runtime is not touched until Run, so registration stays side-effect free.
func newStatusRuntimeProbeCheck(cityPath string, cfg *config.City) *statusRuntimeProbeCheck {
	c := &statusRuntimeProbeCheck{cityPath: cityPath, cfg: cfg}
	c.probe = c.runProbe
	return c
}

// Name implements doctor.Check.
func (*statusRuntimeProbeCheck) Name() string { return "status-runtime-probe" }

// CanFix implements doctor.Check. The remedy is a code-level bound or a
// runtime repair, so the check is diagnostic-only.
func (*statusRuntimeProbeCheck) CanFix() bool { return false }

// WarmupEligible implements doctor.Check. The probe forks a runtime
// subprocess, so it stays out of the gc start warm-up scan.
func (*statusRuntimeProbeCheck) WarmupEligible() bool { return false }

// Fix implements doctor.Check.
func (*statusRuntimeProbeCheck) Fix(_ *doctor.CheckContext) error { return nil }

// Run implements doctor.Check.
func (c *statusRuntimeProbeCheck) Run(_ *doctor.CheckContext) *doctor.CheckResult {
	outcome, err := c.probe()
	if err != nil {
		return &doctor.CheckResult{
			Name:     c.Name(),
			Status:   doctor.StatusWarning,
			Severity: doctor.SeverityAdvisory,
			Message:  fmt.Sprintf("runtime status probe could not run: %v", err),
			FixHint:  "resolve the session provider error above; gc status observes agents through this same provider",
		}
	}

	headroom := statusProviderCallTimeout / statusRuntimeProbeHeadroomDivisor
	elapsed := outcome.Elapsed.Round(time.Millisecond)
	details := []string{
		fmt.Sprintf("probed session: %s", outcome.SessionName),
		fmt.Sprintf("probe answered in: %s", elapsed),
		fmt.Sprintf("probe bound (statusProviderCallTimeout): %s", statusProviderCallTimeout),
	}

	if outcome.Partial {
		return &doctor.CheckResult{
			Name:     c.Name(),
			Status:   doctor.StatusError,
			Severity: doctor.SeverityAdvisory,
			Message:  fmt.Sprintf("runtime status probe exceeded %s; gc status reports partial status and renders agents as unknown", statusProviderCallTimeout),
			Details:  details,
			FixHint:  "check runtime responsiveness (for tmux: gc doctor tmux, then time a list-panes against the city socket); a healthy runtime that still exceeds the bound means the bound is too tight for this host",
		}
	}

	if outcome.Elapsed > headroom {
		return &doctor.CheckResult{
			Name:     c.Name(),
			Status:   doctor.StatusWarning,
			Severity: doctor.SeverityAdvisory,
			Message:  fmt.Sprintf("runtime status probe answered in %s, past %s of its %s bound", elapsed, headroom, statusProviderCallTimeout),
			Details:  details,
			FixHint:  "the probe is close enough to its bound that load will trip it and blank gc status; investigate runtime latency before it does",
		}
	}

	return okCheck(c.Name(), fmt.Sprintf("runtime status probe answered in %s (bound %s)", elapsed, statusProviderCallTimeout))
}

// runProbe observes one session through the same bounded status provider
// gc status builds, and reports how long the runtime took plus whether the
// bound was tripped.
func (c *statusRuntimeProbeCheck) runProbe() (statusRuntimeProbeOutcome, error) {
	sp, err := newStatusSessionProviderForCity(c.cfg, c.cityPath)
	if err != nil {
		return statusRuntimeProbeOutcome{}, err
	}
	name := statusRuntimeProbeSessionName(c.cityPath, c.cfg)
	start := time.Now()
	runtime.ObserveLiveness(sp, name, nil)
	return statusRuntimeProbeOutcome{
		SessionName: name,
		Elapsed:     time.Since(start),
		Partial:     statusProviderPartial(sp),
	}, nil
}

// statusRuntimeProbeSessionName returns the runtime session name to probe: the
// first configured agent's, so the probe names a target gc status would also
// observe, falling back to a synthetic name for a city with no agents.
func statusRuntimeProbeSessionName(cityPath string, cfg *config.City) string {
	if cfg == nil {
		return statusRuntimeProbeFallbackSession
	}
	cityName := loadedCityName(cfg, cityPath)
	for i := range cfg.Agents {
		name := agent.SessionNameFor(cityName, cfg.Agents[i].QualifiedName(), cfg.Workspace.SessionTemplate)
		if name != "" {
			return name
		}
	}
	return statusRuntimeProbeFallbackSession
}
