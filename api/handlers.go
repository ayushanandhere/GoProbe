package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ayushanandhere/GoProbe/config"
	"github.com/ayushanandhere/GoProbe/monitor"
)

type healthResponse struct {
	Status string `json:"status"`
	Uptime string `json:"uptime"`
}

type targetsResponse struct {
	Targets []any `json:"targets"`
}

type createTargetRequest struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Endpoint string `json:"endpoint"`
	Interval string `json:"interval"`
	Timeout  string `json:"timeout"`
}

type messageResponse struct {
	Message string `json:"message"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		Status: "ok",
		Uptime: time.Since(s.startedAt).Round(time.Second).String(),
	})
}

func (s *Server) handleListTargets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"targets": s.monitor.GetAllStatuses(),
	})
}

func (s *Server) handleGetTarget(w http.ResponseWriter, r *http.Request) {
	name, err := decodeTargetName(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid target name")
		return
	}

	status, ok := s.monitor.GetStatus(name)
	if !ok {
		writeError(w, http.StatusNotFound, "target not found")
		return
	}

	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleCreateTarget(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeMutation(w, r) {
		return
	}

	var req createTargetRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	target, err := s.buildTarget(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	status, err := s.monitor.AddTarget(target)
	if err != nil {
		switch {
		case errors.Is(err, monitor.ErrTargetAlreadyExists):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, monitor.ErrMonitorStopped):
			writeError(w, http.StatusServiceUnavailable, err.Error())
		default:
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusCreated, status)
}

func (s *Server) handleDeleteTarget(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeMutation(w, r) {
		return
	}

	name, err := decodeTargetName(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid target name")
		return
	}

	if err := s.monitor.RemoveTarget(name); err != nil {
		if errors.Is(err, monitor.ErrTargetNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, messageResponse{Message: "target removed"})
}

func (s *Server) buildTarget(req createTargetRequest) (config.Target, error) {
	target := config.Target{
		Name:     req.Name,
		Type:     req.Type,
		Endpoint: req.Endpoint,
	}
	config.NormalizeTarget(&target)

	if req.Interval != "" {
		interval, err := time.ParseDuration(req.Interval)
		if err != nil {
			return target, errors.New("invalid interval")
		}
		target.Interval = interval
	}

	if req.Timeout != "" {
		timeout, err := time.ParseDuration(req.Timeout)
		if err != nil {
			return target, errors.New("invalid timeout")
		}
		target.Timeout = timeout
	}

	config.ApplyTargetDefaults(&target, s.defaults)
	if err := config.ValidateTarget(target); err != nil {
		return target, err
	}

	return target, nil
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	writeJSON(w, statusCode, errorResponse{Error: message})
}

func decodeTargetName(value string) (string, error) {
	name, err := url.PathUnescape(value)
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("empty target name")
	}
	return name, nil
}

func (s *Server) authorizeMutation(w http.ResponseWriter, r *http.Request) bool {
	const challenge = `Bearer realm="goprobe"`
	const prefix = "Bearer "

	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader == "" {
		w.Header().Set("WWW-Authenticate", challenge)
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return false
	}
	if !strings.HasPrefix(authHeader, prefix) {
		w.Header().Set("WWW-Authenticate", challenge)
		writeError(w, http.StatusUnauthorized, "invalid bearer token")
		return false
	}

	token := strings.TrimSpace(strings.TrimPrefix(authHeader, prefix))
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) != 1 {
		w.Header().Set("WWW-Authenticate", challenge)
		writeError(w, http.StatusUnauthorized, "invalid bearer token")
		return false
	}

	return true
}
