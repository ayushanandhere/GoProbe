package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ayushanandhere/GoProbe/config"
	"github.com/ayushanandhere/GoProbe/monitor"
)

type Server struct {
	port      int
	startedAt time.Time
	logger    *slog.Logger
	monitor   *monitor.Monitor
	defaults  config.MonitorConfig
	mux       *http.ServeMux
	server    *http.Server
}

func NewServer(port int, defaults config.MonitorConfig, mon *monitor.Monitor, logger *slog.Logger) *Server {
	s := &Server{
		port:      port,
		startedAt: time.Now(),
		logger:    logger,
		monitor:   mon,
		defaults:  defaults,
		mux:       http.NewServeMux(),
	}

	s.registerRoutes()
	s.server = &http.Server{
		Addr:    s.address(),
		Handler: s.mux,
	}

	return s
}

func (s *Server) Start() error {
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /api/targets", s.handleListTargets)
	s.mux.HandleFunc("POST /api/targets", s.handleCreateTarget)
	s.mux.HandleFunc("GET /api/targets/{name}", s.handleGetTarget)
	s.mux.HandleFunc("DELETE /api/targets/{name}", s.handleDeleteTarget)
}

func (s *Server) address() string {
	return ":" + strconv.Itoa(s.port)
}
