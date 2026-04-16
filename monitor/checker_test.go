package monitor

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ayushanandhere/GoProbe/config"
)

func TestCheckHTTP(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		statusCode, responseTime, err := CheckHTTP(context.Background(), testHTTPCheckerTarget(srv.URL))
		if err != nil {
			t.Fatalf("CheckHTTP() error = %v", err)
		}
		if statusCode != http.StatusOK {
			t.Fatalf("statusCode = %d, want 200", statusCode)
		}
		if responseTime <= 0 {
			t.Fatalf("responseTime = %v, want > 0", responseTime)
		}
	})

	t.Run("server error still returns status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		statusCode, _, err := CheckHTTP(context.Background(), testHTTPCheckerTarget(srv.URL))
		if err != nil {
			t.Fatalf("CheckHTTP() error = %v", err)
		}
		if statusCode != http.StatusInternalServerError {
			t.Fatalf("statusCode = %d, want 500", statusCode)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		target := testHTTPCheckerTarget(srv.URL)
		target.Timeout = 20 * time.Millisecond

		_, _, err := CheckHTTP(context.Background(), target)
		if err == nil {
			t.Fatal("CheckHTTP() error = nil, want timeout")
		}
	})

	t.Run("untrusted redirect target is rejected", func(t *testing.T) {
		err := validateRedirectTarget(&url.URL{
			Scheme: "http",
			Host:   "127.0.0.1:8080",
		})
		if err == nil {
			t.Fatal("validateRedirectTarget() error = nil, want rejection")
		}
	})
}

func TestCheckTCP(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Listen() error = %v", err)
		}
		defer listener.Close()

		done := make(chan struct{})
		go func() {
			defer close(done)
			conn, err := listener.Accept()
			if err == nil {
				conn.Close()
			}
		}()

		responseTime, err := CheckTCP(context.Background(), config.Target{
			Name:     "listener",
			Type:     "tcp",
			Endpoint: listener.Addr().String(),
			Interval: time.Second,
			Timeout:  time.Second,
			Trusted:  true,
		})
		if err != nil {
			t.Fatalf("CheckTCP() error = %v", err)
		}
		if responseTime <= 0 {
			t.Fatalf("responseTime = %v, want > 0", responseTime)
		}
		<-done
	})

	t.Run("refused", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Listen() error = %v", err)
		}
		addr := listener.Addr().String()
		listener.Close()

		_, err = CheckTCP(context.Background(), config.Target{
			Name:     "listener",
			Type:     "tcp",
			Endpoint: addr,
			Interval: time.Second,
			Timeout:  100 * time.Millisecond,
			Trusted:  true,
		})
		if err == nil {
			t.Fatal("CheckTCP() error = nil, want refusal")
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			t.Fatalf("CheckTCP() error = %v, want connection refusal", err)
		}
	})
}

func testHTTPCheckerTarget(endpoint string) config.Target {
	return config.Target{
		Name:     "http-target",
		Type:     "http",
		Endpoint: endpoint,
		Interval: time.Second,
		Timeout:  time.Second,
		Trusted:  true,
	}
}
