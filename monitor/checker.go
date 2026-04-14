package monitor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

func CheckHTTP(endpoint string, timeout time.Duration) (statusCode int, responseTime time.Duration, err error) {
	client := &http.Client{Timeout: timeout}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, nil)
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

	return resp.StatusCode, responseTime, nil
}

func CheckTCP(endpoint string, timeout time.Duration) (responseTime time.Duration, err error) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", endpoint, timeout)
	responseTime = time.Since(start)
	if err != nil {
		return responseTime, err
	}
	defer conn.Close()

	return responseTime, nil
}
