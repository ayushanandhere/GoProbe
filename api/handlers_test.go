package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ayushanandhere/GoProbe/config"
	"github.com/ayushanandhere/GoProbe/monitor"
)

func TestHealthEndpoint(t *testing.T) {
	srv, cleanup := newTestServer(t, nil)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

func TestListTargets(t *testing.T) {
	healthSrv := newHealthyHTTPServer(t)
	defer healthSrv.Close()

	targets := []config.Target{{
		Name:     "example",
		Type:     "http",
		Endpoint: healthSrv.URL,
		Interval: 50 * time.Millisecond,
		Timeout:  time.Second,
	}}

	srv, cleanup := newTestServer(t, targets)
	defer cleanup()

	waitFor(t, time.Second, func() bool {
		status, ok := srv.monitor.GetStatus("example")
		return ok && status.CheckCount > 0
	})

	req := httptest.NewRequest(http.MethodGet, "/api/targets", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Targets []map[string]any `json:"targets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(body.Targets) != 1 {
		t.Fatalf("len(targets) = %d, want 1", len(body.Targets))
	}
}

func TestGetTargetNotFound(t *testing.T) {
	srv, cleanup := newTestServer(t, nil)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/targets/missing", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGetTarget(t *testing.T) {
	healthSrv := newHealthyHTTPServer(t)
	defer healthSrv.Close()

	srv, cleanup := newTestServer(t, []config.Target{{
		Name:     "Example Service",
		Type:     "http",
		Endpoint: healthSrv.URL,
		Interval: 50 * time.Millisecond,
		Timeout:  time.Second,
	}})
	defer cleanup()

	waitFor(t, time.Second, func() bool {
		status, ok := srv.monitor.GetStatus("Example Service")
		return ok && status.CheckCount > 0
	})

	req := httptest.NewRequest(http.MethodGet, "/api/targets/Example%20Service", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got := body["name"]; got != "Example Service" {
		t.Fatalf("name = %v, want Example Service", got)
	}
}

func TestCreateTarget(t *testing.T) {
	srv, cleanup := newTestServer(t, nil)
	defer cleanup()

	body := []byte(`{"name":"Example","type":"http","endpoint":"https://example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+srv.AuthToken())
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	if _, ok := srv.monitor.GetStatus("Example"); !ok {
		t.Fatal("GetStatus() ok = false, want true")
	}
}

func TestCreateTargetBadRequest(t *testing.T) {
	srv, cleanup := newTestServer(t, nil)
	defer cleanup()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing name",
			body: `{"type":"http","endpoint":"https://example.com"}`,
		},
		{
			name: "invalid interval",
			body: `{"name":"Example","type":"http","endpoint":"https://example.com","interval":"not-a-duration"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Authorization", "Bearer "+srv.AuthToken())
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
		})
	}
}

func TestCreateTargetRequiresAuthorization(t *testing.T) {
	srv, cleanup := newTestServer(t, nil)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader([]byte(`{"name":"Example","type":"http","endpoint":"https://example.com"}`)))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestCreateTargetRejectsUnknownFields(t *testing.T) {
	srv, cleanup := newTestServer(t, nil)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader([]byte(`{"name":"Example","type":"http","endpoint":"https://example.com","extra":"nope"}`)))
	req.Header.Set("Authorization", "Bearer "+srv.AuthToken())
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateTargetRejectsTrailingJSON(t *testing.T) {
	srv, cleanup := newTestServer(t, nil)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader([]byte(`{"name":"Example","type":"http","endpoint":"https://example.com"}{"extra":true}`)))
	req.Header.Set("Authorization", "Bearer "+srv.AuthToken())
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateTargetDuplicate(t *testing.T) {
	healthSrv := newHealthyHTTPServer(t)
	defer healthSrv.Close()

	srv, cleanup := newTestServer(t, []config.Target{{
		Name:     "Example",
		Type:     "http",
		Endpoint: healthSrv.URL,
		Interval: 50 * time.Millisecond,
		Timeout:  time.Second,
	}})
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader([]byte(`{"name":"Example","type":"http","endpoint":"https://example.com"}`)))
	req.Header.Set("Authorization", "Bearer "+srv.AuthToken())
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestDeleteTarget(t *testing.T) {
	healthSrv := newHealthyHTTPServer(t)
	defer healthSrv.Close()

	srv, cleanup := newTestServer(t, []config.Target{{
		Name:     "Example",
		Type:     "http",
		Endpoint: healthSrv.URL,
		Interval: 30 * time.Millisecond,
		Timeout:  time.Second,
	}})
	defer cleanup()

	waitFor(t, time.Second, func() bool {
		status, ok := srv.monitor.GetStatus("Example")
		return ok && status.CheckCount > 0
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/targets/Example", nil)
	req.Header.Set("Authorization", "Bearer "+srv.AuthToken())
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if _, ok := srv.monitor.GetStatus("Example"); ok {
		t.Fatal("GetStatus() ok = true, want false after delete")
	}
}

func TestDeleteTargetNotFound(t *testing.T) {
	srv, cleanup := newTestServer(t, nil)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/targets/missing", nil)
	req.Header.Set("Authorization", "Bearer "+srv.AuthToken())
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestServerAppliesHTTPLimits(t *testing.T) {
	srv, cleanup := newTestServer(t, nil)
	defer cleanup()

	if srv.server.ReadHeaderTimeout != readHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %v, want %v", srv.server.ReadHeaderTimeout, readHeaderTimeout)
	}
	if srv.server.ReadTimeout != readTimeout {
		t.Fatalf("ReadTimeout = %v, want %v", srv.server.ReadTimeout, readTimeout)
	}
	if srv.server.WriteTimeout != writeTimeout {
		t.Fatalf("WriteTimeout = %v, want %v", srv.server.WriteTimeout, writeTimeout)
	}
	if srv.server.IdleTimeout != idleTimeout {
		t.Fatalf("IdleTimeout = %v, want %v", srv.server.IdleTimeout, idleTimeout)
	}
	if srv.server.MaxHeaderBytes != maxHeaderBytes {
		t.Fatalf("MaxHeaderBytes = %d, want %d", srv.server.MaxHeaderBytes, maxHeaderBytes)
	}
}

func newTestServer(t *testing.T, targets []config.Target) (*Server, func()) {
	t.Helper()

	mon := monitor.NewMonitor(targets, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mon.Start()

	srv := NewServer(config.ServerConfig{
		Port:      8080,
		AuthToken: "test-token",
	}, config.MonitorConfig{
		DefaultInterval: time.Second,
		DefaultTimeout:  time.Second,
	}, mon, slog.New(slog.NewTextHandler(io.Discard, nil)))

	return srv, mon.Stop
}

func newHealthyHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
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
