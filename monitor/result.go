package monitor

import "time"

const historyWindowSize = 100

type HealthStatus struct {
	Name           string    `json:"name"`
	Type           string    `json:"type"`
	Endpoint       string    `json:"endpoint"`
	IsHealthy      bool      `json:"is_healthy"`
	ResponseTimeMS int64     `json:"response_time_ms"`
	StatusCode     int       `json:"status_code,omitempty"`
	Error          string    `json:"error,omitempty"`
	LastChecked    time.Time `json:"last_checked"`
	UptimePercent  float64   `json:"uptime_percent"`
	CheckCount     int       `json:"check_count"`
	FailCount      int       `json:"fail_count"`
}

type CheckResult struct {
	Name         string
	Type         string
	Endpoint     string
	IsHealthy    bool
	ResponseTime time.Duration
	StatusCode   int
	Error        string
	LastChecked  time.Time
}

type historyBuffer struct {
	values []bool
}

func newHistoryBuffer() historyBuffer {
	return historyBuffer{values: make([]bool, 0, historyWindowSize)}
}

func (h *historyBuffer) Record(healthy bool) {
	if len(h.values) == historyWindowSize {
		copy(h.values, h.values[1:])
		h.values[len(h.values)-1] = healthy
		return
	}

	h.values = append(h.values, healthy)
}

func (h historyBuffer) UptimePercent() float64 {
	if len(h.values) == 0 {
		return 0
	}

	healthyCount := 0
	for _, value := range h.values {
		if value {
			healthyCount++
		}
	}

	return float64(healthyCount) / float64(len(h.values)) * 100
}
