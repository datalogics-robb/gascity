package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/doctor"
)

// stubBounderLookPath points initLookPath at a fixed set of installed
// binaries for the duration of the test.
func stubBounderLookPath(t *testing.T, installed ...string) {
	t.Helper()
	present := make(map[string]bool, len(installed))
	for _, name := range installed {
		present[name] = true
	}
	old := initLookPath
	initLookPath = func(name string) (string, error) {
		if present[name] {
			return "/usr/bin/" + name, nil
		}
		return "", os.ErrNotExist
	}
	t.Cleanup(func() { initLookPath = old })
}

func TestDiagnosticBounderCheckResolvesInRunBoundedPreferenceOrder(t *testing.T) {
	tests := []struct {
		name      string
		installed []string
		want      string
	}{
		{
			name:      "prefers gtimeout when every bounder is installed",
			installed: []string{"gtimeout", "timeout", "python3"},
			want:      "bounded diagnostics available via gtimeout",
		},
		{
			name:      "falls back to timeout when gtimeout is absent",
			installed: []string{"timeout", "python3"},
			want:      "bounded diagnostics available via timeout",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubBounderLookPath(t, tt.installed...)

			r := newDiagnosticBounderCheck().Run(&doctor.CheckContext{})
			if r.Status != doctor.StatusOK {
				t.Fatalf("status = %v, want StatusOK", r.Status)
			}
			if r.Message != tt.want {
				t.Fatalf("message = %q, want %q", r.Message, tt.want)
			}
			if r.FixHint != "" {
				t.Fatalf("FixHint = %q, want empty for a satisfied check", r.FixHint)
			}
		})
	}
}

func TestDiagnosticBounderCheckPython3OnlyPassesAndNamesTheGap(t *testing.T) {
	stubBounderLookPath(t, "python3")

	r := newDiagnosticBounderCheck().Run(&doctor.CheckContext{})
	// python3 satisfies run_bounded, so this must not warn: warning here
	// would fire on every stock macOS host that is in fact fine.
	if r.Status != doctor.StatusOK {
		t.Fatalf("status = %v, want StatusOK (python3 satisfies run_bounded)", r.Status)
	}
	if want := "bounded diagnostics available via the python3 fallback (no gtimeout/timeout on PATH)"; r.Message != want {
		t.Fatalf("message = %q, want %q", r.Message, want)
	}
	// The residual exposure — literal `timeout ...` command lines still
	// fail — belongs in the details, not in a warning.
	if len(r.Details) == 0 {
		t.Fatal("Details = empty, want the literal-timeout caveat")
	}
	if !strings.Contains(strings.Join(r.Details, "\n"), "timeout") {
		t.Fatalf("Details = %q, want the literal-timeout caveat", r.Details)
	}
}

func TestDiagnosticBounderCheckWarnsWhenNoBounderIsInstalled(t *testing.T) {
	stubBounderLookPath(t)

	r := newDiagnosticBounderCheck().Run(&doctor.CheckContext{})
	if r.Status != doctor.StatusWarning {
		t.Fatalf("status = %v, want StatusWarning", r.Status)
	}
	// Optional-with-warning: a missing bounder degrades diagnostics, it
	// does not break the town, so it must never gate the exit code.
	if r.Severity != doctor.SeverityAdvisory {
		t.Fatalf("severity = %v, want SeverityAdvisory", r.Severity)
	}
	for _, name := range []string{"gtimeout", "timeout", "python3"} {
		if !strings.Contains(r.Message, name) {
			t.Fatalf("message = %q, want it to name %q", r.Message, name)
		}
	}
	if r.FixHint == "" {
		t.Fatal("FixHint = empty, want an install hint")
	}
	if len(r.Details) == 0 {
		t.Fatal("Details = empty, want the false-hang-signal explanation")
	}
}

func TestDiagnosticBounderCheckMetadata(t *testing.T) {
	c := newDiagnosticBounderCheck()
	if got := c.Name(); got != "diagnostic-bounder" {
		t.Fatalf("Name() = %q, want %q", got, "diagnostic-bounder")
	}
	if c.CanFix() {
		t.Fatal("CanFix() = true, want false — installing a bounder is an operator action")
	}
	if err := c.Fix(&doctor.CheckContext{}); err != nil {
		t.Fatalf("Fix() = %v, want nil", err)
	}
}

func TestDoDoctorRegistersDiagnosticBounderCheckForBdContract(t *testing.T) {
	skipSlowCmdGCTest(t, "runs the full doctor check set")
	cityDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(`[workspace]
name = "demo"

[beads]
provider = "bd"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GC_CITY_PATH", cityDir)
	t.Setenv("GC_DOLT", "skip")
	cleanupManagedDoltTestCity(t, cityDir)

	var stdout, stderr bytes.Buffer
	_ = doDoctor(false, false, false, 0, &stdout, &stderr)

	if out := stdout.String() + stderr.String(); !strings.Contains(out, "diagnostic-bounder") {
		t.Fatalf("doctor output missing diagnostic-bounder check:\n%s", out)
	}
}

func TestDoDoctorSkipsDiagnosticBounderCheckForFileBackedCity(t *testing.T) {
	skipSlowCmdGCTest(t, "runs the full doctor check set")
	cityDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(`[workspace]
name = "demo"

[beads]
provider = "file"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GC_CITY_PATH", cityDir)
	t.Setenv("GC_DOLT", "skip")
	cleanupManagedDoltTestCity(t, cityDir)

	var stdout, stderr bytes.Buffer
	_ = doDoctor(false, false, false, 0, &stdout, &stderr)

	out := stdout.String() + stderr.String()
	// Positive control: the run must have actually produced check output,
	// so the absence asserted below means "not registered", not "no run".
	if !strings.Contains(out, "tmux-binary") {
		t.Fatalf("doctor produced no check output; absence below would be vacuous:\n%s", out)
	}
	// A file-backed city has no bd/Dolt data plane, so no bounded
	// diagnostics to be exposed about.
	if strings.Contains(out, "diagnostic-bounder") {
		t.Fatalf("doctor registered diagnostic-bounder for a file-backed city:\n%s", out)
	}
}
