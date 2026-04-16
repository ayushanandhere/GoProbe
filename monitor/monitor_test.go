package monitor

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ayushanandhere/GoProbe/config"
)

func TestMonitorStartUpdatesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	m := NewMonitor([]config.Target{{
		Name:     "example",
		Type:     "http",
		Endpoint: srv.URL,
		Interval: 50 * time.Millisecond,
		Timeout:  time.Second,
	}}, testLogger())
	defer m.Stop()

	m.Start()

	waitFor(t, time.Second, func() bool {
		status, ok := m.GetStatus("example")
		return ok && status.CheckCount > 0
	})

	status, ok := m.GetStatus("example")
	if !ok {
		t.Fatal("GetStatus() ok = false, want true")
	}
	if !status.IsHealthy {
		t.Fatalf("status.IsHealthy = false, want true")
	}
}

func TestGetAllStatusesReturnsSnapshot(t *testing.T) {
	m := NewMonitor([]config.Target{{
		Name:     "one",
		Type:     "tcp",
		Endpoint: "localhost:1234",
		Interval: time.Second,
		Timeout:  time.Second,
	}}, testLogger())

	statuses := m.GetAllStatuses()
	if len(statuses) != 1 {
		t.Fatalf("len(statuses) = %d, want 1", len(statuses))
	}

	statuses[0].Name = "mutated"
	status, ok := m.GetStatus("one")
	if !ok {
		t.Fatal("GetStatus() ok = false, want true")
	}
	if status.Name != "one" {
		t.Fatalf("status.Name = %q, want %q", status.Name, "one")
	}
}

func TestAddTargetStartsPollingImmediately(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	m := NewMonitor(nil, testLogger())
	defer m.Stop()
	m.Start()

	_, err := m.AddTarget(config.Target{
		Name:     "dynamic",
		Type:     "http",
		Endpoint: srv.URL,
		Interval: time.Second,
		Timeout:  time.Second,
		Trusted:  true,
	})
	if err != nil {
		t.Fatalf("AddTarget() error = %v", err)
	}

	waitFor(t, time.Second, func() bool {
		status, ok := m.GetStatus("dynamic")
		return ok && status.CheckCount > 0
	})
}

func TestRemoveTargetStopsPolling(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	m := NewMonitor([]config.Target{{
		Name:     "dynamic",
		Type:     "http",
		Endpoint: srv.URL,
		Interval: 30 * time.Millisecond,
		Timeout:  time.Second,
	}}, testLogger())
	defer m.Stop()
	m.Start()

	waitFor(t, time.Second, func() bool {
		status, ok := m.GetStatus("dynamic")
		return ok && status.CheckCount > 1
	})

	before := hits.Load()
	if err := m.RemoveTarget("dynamic"); err != nil {
		t.Fatalf("RemoveTarget() error = %v", err)
	}

	time.Sleep(120 * time.Millisecond)

	if _, ok := m.GetStatus("dynamic"); ok {
		t.Fatal("GetStatus() ok = true, want false after removal")
	}

	after := hits.Load()
	if after > before+1 {
		t.Fatalf("hits after removal = %d, want at most %d", after, before+1)
	}
}

func TestStopDoesNotHang(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	m := NewMonitor([]config.Target{{
		Name:     "example",
		Type:     "http",
		Endpoint: srv.URL,
		Interval: 50 * time.Millisecond,
		Timeout:  time.Second,
	}}, testLogger())
	m.Start()

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.Stop()
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop() timed out")
	}
}

func TestApplyResultUpdatesUptime(t *testing.T) {
	m := NewMonitor([]config.Target{{
		Name:     "example",
		Type:     "http",
		Endpoint: "https://example.com",
		Interval: time.Second,
		Timeout:  time.Second,
	}}, testLogger())

	now := time.Now()
	m.applyResult(CheckResult{Name: "example", Type: "http", Endpoint: "https://example.com", IsHealthy: true, ResponseTime: 10 * time.Millisecond, StatusCode: 200, LastChecked: now})
	m.applyResult(CheckResult{Name: "example", Type: "http", Endpoint: "https://example.com", IsHealthy: true, ResponseTime: 10 * time.Millisecond, StatusCode: 200, LastChecked: now})
	m.applyResult(CheckResult{Name: "example", Type: "http", Endpoint: "https://example.com", IsHealthy: false, ResponseTime: 10 * time.Millisecond, StatusCode: 500, Error: "received non-2xx response", LastChecked: now})
	m.applyResult(CheckResult{Name: "example", Type: "http", Endpoint: "https://example.com", IsHealthy: true, ResponseTime: 10 * time.Millisecond, StatusCode: 200, LastChecked: now})

	status, ok := m.GetStatus("example")
	if !ok {
		t.Fatal("GetStatus() ok = false, want true")
	}
	if status.CheckCount != 4 {
		t.Fatalf("CheckCount = %d, want 4", status.CheckCount)
	}
	if status.FailCount != 1 {
		t.Fatalf("FailCount = %d, want 1", status.FailCount)
	}
	if status.UptimePercent != 75 {
		t.Fatalf("UptimePercent = %v, want 75", status.UptimePercent)
	}
}

func waitFor(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("condition not met before timeout")
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
