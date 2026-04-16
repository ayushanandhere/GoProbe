package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ayushanandhere/GoProbe/config"
	"github.com/ayushanandhere/GoProbe/monitor"
)

type Server struct {
	port      int
	authToken string
	startedAt time.Time
	logger    *slog.Logger
	monitor   *monitor.Monitor
	defaults  config.MonitorConfig
	mux       *http.ServeMux
	server    *http.Server
}

const (
	maxRequestBodyBytes = 1 << 20
	readHeaderTimeout   = 2 * time.Second
	readTimeout         = 5 * time.Second
	writeTimeout        = 10 * time.Second
	idleTimeout         = 60 * time.Second
	maxHeaderBytes      = 1 << 20
)

func NewServer(serverConfig config.ServerConfig, defaults config.MonitorConfig, mon *monitor.Monitor, logger *slog.Logger) *Server {
	authToken := strings.TrimSpace(serverConfig.AuthToken)
	if authToken == "" {
		authToken = mustGenerateAuthToken()
		logger.Warn("generated ephemeral API token for mutating endpoints", "token", authToken)
	}

	s := &Server{
		port:      serverConfig.Port,
		authToken: authToken,
		startedAt: time.Now(),
		logger:    logger,
		monitor:   mon,
		defaults:  defaults,
		mux:       http.NewServeMux(),
	}

	s.registerRoutes()
	s.server = &http.Server{
		Addr:              s.address(),
		Handler:           http.MaxBytesHandler(s.mux, maxRequestBodyBytes),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
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

func (s *Server) AuthToken() string {
	return s.authToken
}

func mustGenerateAuthToken() string {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		panic(err)
	}
	return hex.EncodeToString(token)
}
