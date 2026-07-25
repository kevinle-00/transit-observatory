package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/database"
)

type HealthChecker interface {
	PingContext(context.Context) error
}

type AlertLister interface {
	List(context.Context) ([]database.CurrentAlert, error)
}

type Handler struct {
	healthChecker  HealthChecker
	alerts         AlertLister
	logger         *slog.Logger
	allowedOrigin  string
	requestTimeout time.Duration
}

type errorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type alertsResponse struct {
	Data []database.CurrentAlert `json:"data"`
	Meta collectionMeta          `json:"meta"`
}

type collectionMeta struct {
	Count int `json:"count"`
}

func NewHandler(healthChecker HealthChecker, alerts AlertLister, logger *slog.Logger, allowedOrigin string, requestTimeout time.Duration) http.Handler {
	return &Handler{
		healthChecker:  healthChecker,
		alerts:         alerts,
		logger:         logger,
		allowedOrigin:  allowedOrigin,
		requestTimeout: requestTimeout,
	}
}

func (h *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Cache-Control", "no-store")
	h.applyCORS(response, request)

	switch request.URL.Path {
	case "/health":
	case "/api/v1/alerts":
	default:
		h.writeError(response, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if request.Method == http.MethodOptions {
		response.Header().Set("Allow", "GET, OPTIONS")
		h.handleOptions(response, request)
		return
	}
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", "GET, OPTIONS")
		h.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if request.URL.Path == "/health" {
		h.handleHealth(response, request)
	} else {
		h.handleAlerts(response, request)
	}
}

func (h *Handler) handleHealth(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		h.writeError(response, http.StatusBadRequest, "invalid_query", "query parameters are not supported")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	if err := h.healthChecker.PingContext(ctx); err != nil {
		h.logger.WarnContext(request.Context(), "API health check failed", "error", err)
		h.writeJSON(response, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	h.writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleAlerts(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		h.writeError(response, http.StatusBadRequest, "invalid_query", "query parameters are not supported")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), h.requestTimeout)
	defer cancel()
	alerts, err := h.alerts.List(ctx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			h.logger.WarnContext(request.Context(), "current alert query timed out", "error", err)
			h.writeError(response, http.StatusGatewayTimeout, "request_timeout", "request timed out")
			return
		}
		h.logger.ErrorContext(request.Context(), "current alert query failed", "error", err)
		h.writeError(response, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	h.writeJSON(response, http.StatusOK, alertsResponse{
		Data: alerts,
		Meta: collectionMeta{Count: len(alerts)},
	})
}

func (h *Handler) handleOptions(response http.ResponseWriter, request *http.Request) {
	origin := request.Header.Get("Origin")
	requestedMethod := request.Header.Get("Access-Control-Request-Method")
	if requestedMethod == "" {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	if origin == "" {
		h.writeError(response, http.StatusBadRequest, "invalid_preflight", "invalid CORS preflight request")
		return
	}
	if !h.originAllowed(origin) {
		h.writeError(response, http.StatusForbidden, "origin_not_allowed", "origin not allowed")
		return
	}
	if requestedMethod != http.MethodGet {
		h.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	requestedHeaders := request.Header.Get("Access-Control-Request-Headers")
	if requestedHeaders != "" && !onlyContentTypeRequested(requestedHeaders) {
		h.writeError(response, http.StatusForbidden, "headers_not_allowed", "request headers not allowed")
		return
	}
	response.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	if requestedHeaders != "" {
		response.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	}
	response.Header().Set("Access-Control-Max-Age", "600")
	response.WriteHeader(http.StatusNoContent)
}

func (h *Handler) applyCORS(response http.ResponseWriter, request *http.Request) {
	response.Header().Add("Vary", "Origin")
	origin := request.Header.Get("Origin")
	if origin != "" && h.originAllowed(origin) {
		response.Header().Set("Access-Control-Allow-Origin", h.allowedOrigin)
	}
}

func (h *Handler) originAllowed(origin string) bool {
	return h.allowedOrigin == "*" || origin == h.allowedOrigin
}

func onlyContentTypeRequested(value string) bool {
	for header := range strings.SplitSeq(value, ",") {
		if !strings.EqualFold(strings.TrimSpace(header), "Content-Type") {
			return false
		}
	}
	return true
}

func (h *Handler) writeError(response http.ResponseWriter, status int, code, message string) {
	h.writeJSON(response, status, errorResponse{Error: apiError{Code: code, Message: message}})
}

func (h *Handler) writeJSON(response http.ResponseWriter, status int, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		h.logger.Error("encode API response", "error", err)
		payload = []byte(`{"error":{"code":"internal_error","message":"internal server error"}}`)
		status = http.StatusInternalServerError
	}
	payload = append(payload, '\n')
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	if _, err := response.Write(payload); err != nil {
		h.logger.Error("write API response", "error", err)
	}
}
