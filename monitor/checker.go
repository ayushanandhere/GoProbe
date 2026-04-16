package monitor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ayushanandhere/GoProbe/config"
)

const maxRedirects = 10

func CheckHTTP(ctx context.Context, target config.Target) (statusCode int, responseTime time.Duration, err error) {
	dialer := newTargetDialer(target)
	client := &http.Client{
		Timeout: target.Timeout,
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   target.Timeout,
			ResponseHeaderTimeout: target.Timeout,
		},
		CheckRedirect: dialer.CheckRedirect,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.Endpoint, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("build request: %w", err)
	}

	start := time.Now()
	resp, err := client.Do(req)
	responseTime = time.Since(start)
	if err != nil {
		return 0, responseTime, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	return resp.StatusCode, responseTime, nil
}

func CheckTCP(ctx context.Context, target config.Target) (responseTime time.Duration, err error) {
	dialer := newTargetDialer(target)

	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", target.Endpoint)
	responseTime = time.Since(start)
	if err != nil {
		return responseTime, err
	}
	defer conn.Close()

	return responseTime, nil
}
