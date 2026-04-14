package monitor

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/ayushanandhere/GoProbe/config"
)

var (
	ErrTargetAlreadyExists = errors.New("target already exists")
	ErrTargetNotFound      = errors.New("target not found")
	ErrMonitorStopped      = errors.New("monitor is stopped")
)

type targetState struct {
	target  config.Target
	status  HealthStatus
	history historyBuffer
	cancel  context.CancelFunc
}

type Monitor struct {
	logger *slog.Logger

	mu      sync.RWMutex
	states  map[string]*targetState
	results chan CheckResult

	rootCtx    context.Context
	rootCancel context.CancelFunc

	pollerWG   sync.WaitGroup
	collectorW sync.WaitGroup

	started   bool
	startOnce sync.Once
	stopOnce  sync.Once
}

func NewMonitor(targets []config.Target, logger *slog.Logger) *Monitor {
	rootCtx, rootCancel := context.WithCancel(context.Background())

	m := &Monitor{
		logger:     logger,
		states:     make(map[string]*targetState, len(targets)),
		results:    make(chan CheckResult, max(1, len(targets)*2)),
		rootCtx:    rootCtx,
		rootCancel: rootCancel,
	}

	for _, target := range targets {
		m.states[target.Name] = newTargetState(target)
	}

	return m
}

func (m *Monitor) Start() {
	m.startOnce.Do(func() {
		m.collectorW.Add(1)
		go m.collectResults()

		m.mu.Lock()
		m.started = true
		defer m.mu.Unlock()
		for name := range m.states {
			m.startPollerLocked(name)
		}
	})
}

func (m *Monitor) Stop() {
	m.stopOnce.Do(func() {
		m.rootCancel()

		m.mu.Lock()
		for _, state := range m.states {
			if state.cancel != nil {
				state.cancel()
				state.cancel = nil
			}
		}
		m.mu.Unlock()

		m.pollerWG.Wait()
		close(m.results)
		m.collectorW.Wait()
	})
}

func (m *Monitor) GetAllStatuses() []HealthStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.states))
	for name := range m.states {
		names = append(names, name)
	}
	sort.Strings(names)

	statuses := make([]HealthStatus, 0, len(names))
	for _, name := range names {
		statuses = append(statuses, m.states[name].status)
	}

	return statuses
}

func (m *Monitor) GetStatus(name string) (*HealthStatus, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, ok := m.states[name]
	if !ok {
		return nil, false
	}

	status := state.status
	return &status, true
}

func (m *Monitor) AddTarget(target config.Target) (*HealthStatus, error) {
	config.NormalizeTarget(&target)
	if err := config.ValidateTarget(target); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.rootCtx.Err() != nil {
		return nil, ErrMonitorStopped
	}

	if _, exists := m.states[target.Name]; exists {
		return nil, ErrTargetAlreadyExists
	}

	state := newTargetState(target)
	m.states[target.Name] = state
	if m.started {
		m.startPollerLocked(target.Name)
	}

	m.logger.Info("target added",
		"target", target.Name,
		"type", target.Type,
		"endpoint", target.Endpoint,
	)

	status := state.status
	return &status, nil
}

func (m *Monitor) RemoveTarget(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.states[name]
	if !exists {
		return ErrTargetNotFound
	}

	if state.cancel != nil {
		state.cancel()
		state.cancel = nil
	}
	delete(m.states, name)

	m.logger.Info("target removed", "target", name)
	return nil
}

func (m *Monitor) startPollerLocked(name string) {
	state, exists := m.states[name]
	if !exists || state.cancel != nil {
		return
	}

	ctx, cancel := context.WithCancel(m.rootCtx)
	state.cancel = cancel

	target := state.target
	m.pollerWG.Add(1)
	go m.pollTarget(ctx, target)
}

func (m *Monitor) pollTarget(ctx context.Context, target config.Target) {
	defer m.pollerWG.Done()

	m.performCheck(ctx, target)

	ticker := time.NewTicker(target.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.performCheck(ctx, target)
		}
	}
}

func (m *Monitor) performCheck(ctx context.Context, target config.Target) {
	lastChecked := time.Now()
	result := CheckResult{
		Name:        target.Name,
		Type:        target.Type,
		Endpoint:    target.Endpoint,
		LastChecked: lastChecked,
	}

	switch target.Type {
	case "http":
		statusCode, responseTime, err := CheckHTTP(target.Endpoint, target.Timeout)
		result.StatusCode = statusCode
		result.ResponseTime = responseTime
		result.IsHealthy = err == nil && statusCode >= 200 && statusCode < 300
		if err != nil {
			result.Error = err.Error()
		}
	case "tcp":
		responseTime, err := CheckTCP(target.Endpoint, target.Timeout)
		result.ResponseTime = responseTime
		result.IsHealthy = err == nil
		if err != nil {
			result.Error = err.Error()
		}
	default:
		result.Error = "unsupported target type"
	}

	if !result.IsHealthy && result.Error == "" && target.Type == "http" && result.StatusCode > 0 {
		result.Error = "received non-2xx response"
	}

	select {
	case <-ctx.Done():
		return
	case m.results <- result:
	}
}

func (m *Monitor) collectResults() {
	defer m.collectorW.Done()

	for result := range m.results {
		m.applyResult(result)
	}
}

func (m *Monitor) applyResult(result CheckResult) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.states[result.Name]
	if !exists {
		return
	}

	state.history.Record(result.IsHealthy)

	status := &state.status
	status.IsHealthy = result.IsHealthy
	status.ResponseTimeMS = result.ResponseTime.Milliseconds()
	status.StatusCode = result.StatusCode
	status.Error = result.Error
	status.LastChecked = result.LastChecked
	status.UptimePercent = state.history.UptimePercent()
	status.CheckCount++
	if !result.IsHealthy {
		status.FailCount++
	}

	if result.IsHealthy {
		m.logger.Info("health check completed",
			"target", result.Name,
			"type", result.Type,
			"healthy", result.IsHealthy,
			"response_time_ms", status.ResponseTimeMS,
			"status_code", result.StatusCode,
		)
		return
	}

	m.logger.Warn("health check failed",
		"target", result.Name,
		"type", result.Type,
		"healthy", result.IsHealthy,
		"response_time_ms", status.ResponseTimeMS,
		"status_code", result.StatusCode,
		"error", result.Error,
	)
}

func newTargetState(target config.Target) *targetState {
	return &targetState{
		target: target,
		status: HealthStatus{
			Name:     target.Name,
			Type:     target.Type,
			Endpoint: target.Endpoint,
		},
		history: newHistoryBuffer(),
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
