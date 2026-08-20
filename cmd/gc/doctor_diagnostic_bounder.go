package main

import (
	"fmt"
	"strings"

	"github.com/gastownhall/gascity/internal/doctor"
)

// diagnosticBounderCheck reports whether this host can run a command under a
// wall-clock bound — the mechanism every Dolt diagnostic depends on.
//
// Bounded execution resolves to gtimeout, timeout, or a python3 fallback (see
// run_bounded in the dolt pack's assets/scripts/runtime.sh). With none of them
// on PATH run_bounded fails closed with exit 124, which is indistinguishable
// from a real timeout: bounded probes report a hang against a Dolt server that
// is fine. That false signal lands in the diagnostic procedure operators must
// complete *before* restarting Dolt, so it argues for the evidence-destroying
// restart the procedure exists to prevent.
//
// gc init already refuses to proceed without a bounder, but an
// already-initialized town — every running town — never re-runs that gate.
// This check is the standing one, so a town learns about the exposure before
// an incident rather than during one.
//
// Optional-with-warning (StatusWarning + SeverityAdvisory): a missing bounder
// degrades diagnostics, it does not break the town, so it never gates the
// doctor exit code.
type diagnosticBounderCheck struct{}

func newDiagnosticBounderCheck() *diagnosticBounderCheck { return &diagnosticBounderCheck{} }

// Name returns the check identifier.
func (c *diagnosticBounderCheck) Name() string { return "diagnostic-bounder" }

// CanFix returns false — installing a bounder is an operator action.
func (c *diagnosticBounderCheck) CanFix() bool { return false }

// Fix is a no-op.
func (c *diagnosticBounderCheck) Fix(_ *doctor.CheckContext) error { return nil }

// Run probes the bounder list and reports which mechanism, if any, a bounded
// diagnostic would resolve to on this host.
func (c *diagnosticBounderCheck) Run(_ *doctor.CheckContext) *doctor.CheckResult {
	r := &doctor.CheckResult{Name: c.Name()}

	bounder, ok := initFirstToolAvailable(initDiagnosticBounders...)
	if !ok {
		r.Status = doctor.StatusWarning
		r.Severity = doctor.SeverityAdvisory
		r.Message = fmt.Sprintf("no command bounder on PATH (%s) — bounded Dolt diagnostics cannot run",
			strings.Join(initDiagnosticBounders, ", "))
		r.FixHint = "install GNU coreutils timeout/gtimeout or python3"
		r.Details = []string{
			"Without a bounder, run_bounded fails closed with exit 124 — the same code a real",
			"timeout produces — so every bounded probe reads as a hang even against a healthy Dolt.",
			"That false signal appears in the diagnostics an operator must collect before restarting",
			"Dolt, and argues for the restart those diagnostics exist to prevent.",
		}
		return r
	}

	r.Status = doctor.StatusOK
	if bounder == initBounderPython3 {
		r.Message = "bounded diagnostics available via the python3 fallback (no gtimeout/timeout on PATH)"
		r.Details = []string{
			"run_bounded is satisfied, but a literal `timeout ...` command line is not: it fails with",
			"\"timeout: command not found\", which reads as a hang. Bound commands via run_bounded,",
			"or install GNU coreutils timeout/gtimeout.",
		}
		return r
	}
	r.Message = "bounded diagnostics available via " + bounder
	return r
}
