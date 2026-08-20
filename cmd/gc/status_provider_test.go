package main

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/runtime"
)

type statusProbeProvider struct {
	runtime.Provider
	delay       atomic.Int64
	running     atomic.Bool
	liveness    atomic.Value
	observeCall atomic.Int32
}

func newStatusProbeProvider() *statusProbeProvider {
	p := &statusProbeProvider{Provider: runtime.NewFake()}
	p.liveness.Store(runtime.Liveness{})
	return p
}

func (p *statusProbeProvider) IsRunning(string) bool {
	time.Sleep(time.Duration(p.delay.Load()))
	return p.running.Load()
}

func (p *statusProbeProvider) ObserveLiveness(string, []string) runtime.Liveness {
	p.observeCall.Add(1)
	return p.liveness.Load().(runtime.Liveness)
}

func TestStatusProviderTimeoutDoesNotStickAcrossCalls(t *testing.T) {
	origTimeout := statusProviderCallTimeout
	origWarn := statusProviderTimeoutWarning
	t.Cleanup(func() {
		statusProviderCallTimeout = origTimeout
		statusProviderTimeoutWarning = origWarn
	})
	statusProviderCallTimeout = 10 * time.Millisecond
	var warnings atomic.Int32
	statusProviderTimeoutWarning = func() {
		warnings.Add(1)
	}

	base := newStatusProbeProvider()
	base.running.Store(true)
	base.delay.Store(int64(100 * time.Millisecond))
	wrapped := newBoundedStatusProvider(base)

	if wrapped.IsRunning("worker") {
		t.Fatal("first IsRunning returned true, want timeout fallback false")
	}
	base.delay.Store(0)
	if !wrapped.IsRunning("worker") {
		t.Fatal("second IsRunning returned false, want fresh provider result after timeout")
	}
	if got := warnings.Load(); got != 1 {
		t.Fatalf("timeout warnings = %d, want 1", got)
	}
}

func TestStatusProviderPreservesNativeLivenessObservation(t *testing.T) {
	base := newStatusProbeProvider()
	base.liveness.Store(runtime.Liveness{Running: true, Alive: true})
	wrapped := newBoundedStatusProvider(base)

	got := runtime.ObserveLiveness(wrapped, "worker", []string{"agent"})
	if !got.Running || !got.Alive {
		t.Fatalf("ObserveLiveness = %#v, want running+alive from native observer", got)
	}
	if calls := base.observeCall.Load(); calls != 1 {
		t.Fatalf("ObserveLiveness calls = %d, want 1", calls)
	}
}

func TestStatusProviderTimeoutMarksPartial(t *testing.T) {
	origTimeout := statusProviderCallTimeout
	origWarn := statusProviderTimeoutWarning
	t.Cleanup(func() {
		statusProviderCallTimeout = origTimeout
		statusProviderTimeoutWarning = origWarn
	})
	statusProviderCallTimeout = 10 * time.Millisecond
	statusProviderTimeoutWarning = func() {}

	base := newStatusProbeProvider()
	base.running.Store(true)
	base.delay.Store(int64(100 * time.Millisecond))
	wrapped := newBoundedStatusProvider(base)

	if wrapped.IsRunning("worker") {
		t.Fatal("IsRunning returned true, want timeout fallback false")
	}
	if !statusProviderPartial(wrapped) {
		t.Fatal("statusProviderPartial = false, want true after runtime probe timeout")
	}
}

func TestStatusProbeBoundsClearALoadedRuntimeRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		bound time.Duration
	}{
		{name: "statusProviderCallTimeout", bound: statusProviderCallTimeout},
		{name: "statusObservationTimeout", bound: statusObservationTimeout},
	}
	for _, tc := range tests {
		if tc.bound <= statusProbeLoadedRoundTrip {
			t.Errorf("%s = %s, want above statusProbeLoadedRoundTrip = %s; a bound at or below one loaded round-trip reports a healthy runtime as unresponsive", tc.name, tc.bound, statusProbeLoadedRoundTrip)
		}
	}
}

func TestStatusProbeBoundStaysUnderObservationTimeout(t *testing.T) {
	if statusProviderCallTimeout >= statusObservationTimeout {
		t.Fatalf("statusProviderCallTimeout = %s, want below statusObservationTimeout = %s so the per-observation bound stays the wall-clock backstop", statusProviderCallTimeout, statusObservationTimeout)
	}
}

func TestStatusProbeDoesNotMarkPartialForASlowHealthyRuntime(t *testing.T) {
	origWarn := statusProviderTimeoutWarning
	t.Cleanup(func() { statusProviderTimeoutWarning = origWarn })
	statusProviderTimeoutWarning = func() {}

	base := newStatusProbeProvider()
	base.running.Store(true)
	base.delay.Store(int64(100 * time.Millisecond))
	wrapped := newBoundedStatusProvider(base)

	if !wrapped.IsRunning("worker") {
		t.Fatalf("IsRunning returned false for a live session answering in 100ms; statusProviderCallTimeout = %s is below one runtime round-trip", statusProviderCallTimeout)
	}
	if statusProviderPartial(wrapped) {
		t.Fatalf("statusProviderPartial = true after a 100ms probe; statusProviderCallTimeout = %s marks a healthy runtime partial", statusProviderCallTimeout)
	}
}
