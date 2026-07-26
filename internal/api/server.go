package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kevinle-00/transit-observatory/internal/database"
)

const maxHistoricalAlertPage = 1_000

type HealthChecker interface {
	PingContext(context.Context) error
}

type ReadRepository interface {
	ListAlerts(context.Context, database.AlertQuery) (database.AlertPage, error)
	GetAlert(context.Context, int64) (database.AlertDetail, error)
	ListAlertRevisions(context.Context, int64) ([]database.AlertRevision, error)
	ListLines(context.Context, bool, time.Time) ([]database.LineSummary, error)
	GetLine(context.Context, string, time.Time) (database.LineDetail, error)
	ListStations(context.Context, database.StationQuery, time.Time) ([]database.StationSummary, error)
	GetStation(context.Context, string, time.Time) (database.StationDetail, error)
	ListLineAnalytics(context.Context, database.AnalyticsQuery) ([]database.LineAnalytics, error)
	GetLineAnalytics(context.Context, string, database.AnalyticsQuery) (database.LineAnalytics, error)
	GetStatus(context.Context, database.StatusQuery) (database.StatusResponse, error)
}

type Handler struct {
	healthChecker  HealthChecker
	reads          ReadRepository
	logger         *slog.Logger
	allowedOrigin  string
	requestTimeout time.Duration
	statusQuery    database.StatusQuery
	now            func() time.Time
	mux            *http.ServeMux
}

type errorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type collectionMeta struct {
	Count int `json:"count"`
}

type collectionResponse struct {
	Data any `json:"data"`
	Meta any `json:"meta"`
}

type alertMeta struct {
	Count  int    `json:"count"`
	Status string `json:"status"`
}

type historicalAlertMeta struct {
	Count      int    `json:"count"`
	Status     string `json:"status"`
	Total      int    `json:"total"`
	Page       int    `json:"page"`
	PageSize   int    `json:"page_size"`
	TotalPages int    `json:"total_pages"`
}

type dataResponse struct {
	Data any `json:"data"`
}

type analyticsMeta struct {
	Count       int       `json:"count"`
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	Interval    string    `json:"interval"`
	Timezone    string    `json:"timezone"`
	MetricBasis string    `json:"metric_basis"`
}

type analyticsResponse struct {
	Data any           `json:"data"`
	Meta analyticsMeta `json:"meta"`
}

type statusMeta struct {
	AlertDataMaxAgeSeconds     float64 `json:"alert_data_max_age_seconds"`
	AlertCheckMaxAgeSeconds    float64 `json:"alert_check_max_age_seconds"`
	GTFSDataMaxAgeSeconds      float64 `json:"gtfs_data_max_age_seconds"`
	GTFSCheckMaxAgeSeconds     float64 `json:"gtfs_check_max_age_seconds"`
	AlertRunMaxDurationSeconds float64 `json:"alert_run_max_duration_seconds"`
	GTFSRunMaxDurationSeconds  float64 `json:"gtfs_run_max_duration_seconds"`
	FutureToleranceSeconds     float64 `json:"future_tolerance_seconds"`
	RecentFailureLimit         int     `json:"recent_failure_limit"`
}

type statusResponse struct {
	Data database.StatusResponse `json:"data"`
	Meta statusMeta              `json:"meta"`
}

func NewHandler(healthChecker HealthChecker, reads ReadRepository, logger *slog.Logger, allowedOrigin string, requestTimeout time.Duration, statusQuery database.StatusQuery) *Handler {
	h := &Handler{
		healthChecker:  healthChecker,
		reads:          reads,
		logger:         logger,
		allowedOrigin:  allowedOrigin,
		requestTimeout: requestTimeout,
		statusQuery:    statusQuery,
		now:            time.Now,
		mux:            http.NewServeMux(),
	}
	h.mux.HandleFunc("/health", h.handleHealth)
	h.mux.HandleFunc("/api/v1/alerts", h.handleAlerts)
	h.mux.HandleFunc("/api/v1/alerts/{id}", h.handleAlert)
	h.mux.HandleFunc("/api/v1/alerts/{id}/revisions", h.handleAlertRevisions)
	h.mux.HandleFunc("/api/v1/lines", h.handleLines)
	h.mux.HandleFunc("/api/v1/lines/{id}", h.handleLine)
	h.mux.HandleFunc("/api/v1/stations", h.handleStations)
	h.mux.HandleFunc("/api/v1/stations/{id}", h.handleStation)
	h.mux.HandleFunc("/api/v1/analytics/lines", h.handleLineAnalytics)
	h.mux.HandleFunc("/api/v1/analytics/lines/{id}", h.handleLineAnalyticsDetail)
	h.mux.HandleFunc("/api/v1/status", h.handleStatus)
	return h
}

func (h *Handler) handleStatus(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		h.invalidQuery(response, "query parameters are not supported")
		return
	}
	query := h.statusQuery
	query.Now = h.now().UTC()
	ctx, cancel := context.WithTimeout(request.Context(), h.requestTimeout)
	defer cancel()
	result, err := h.reads.GetStatus(ctx, query)
	if err != nil {
		h.writeRepositoryError(response, request, "status query", err)
		return
	}
	h.writeJSON(response, http.StatusOK, statusResponse{Data: result, Meta: statusMeta{
		AlertDataMaxAgeSeconds: query.AlertDataMaxAge.Seconds(), AlertCheckMaxAgeSeconds: query.AlertCheckMaxAge.Seconds(),
		GTFSDataMaxAgeSeconds: query.GTFSDataMaxAge.Seconds(), GTFSCheckMaxAgeSeconds: query.GTFSCheckMaxAge.Seconds(),
		AlertRunMaxDurationSeconds: query.AlertRunMaxDuration.Seconds(), GTFSRunMaxDurationSeconds: query.GTFSRunMaxDuration.Seconds(),
		FutureToleranceSeconds: query.FutureTolerance.Seconds(), RecentFailureLimit: query.RecentFailureLimit,
	}})
}

func (h *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Cache-Control", "no-store")
	h.applyCORS(response, request)
	if !canonicalRequestPath(request.URL.EscapedPath()) {
		h.writeError(response, http.StatusNotFound, "not_found", "resource not found")
		return
	}

	_, pattern := h.mux.Handler(request)
	if pattern == "" {
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
	h.mux.ServeHTTP(response, request)
}

func canonicalRequestPath(escapedPath string) bool {
	segments := strings.Split(escapedPath, "/")
	for index, segment := range segments {
		if segment == "" && index > 0 && index < len(segments)-1 {
			return false
		}
		decoded, err := url.PathUnescape(segment)
		if err != nil || decoded == "." || decoded == ".." {
			return false
		}
	}
	return true
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
	values, message := parseQuery(request, "status", "line_id", "station_id", "cause", "effect", "from", "to", "page", "page_size")
	if message != "" {
		h.invalidQuery(response, message)
		return
	}
	query := database.AlertQuery{Status: database.AlertStatusPresent, Now: h.now().UTC()}
	if value, present, err := scalar(values, "status"); err != nil {
		h.invalidQuery(response, err.Error())
		return
	} else if present {
		query.Status = value
	}
	switch query.Status {
	case database.AlertStatusPresent, database.AlertStatusCurrent, database.AlertStatusUpcoming, database.AlertStatusHistorical:
	default:
		h.invalidQuery(response, "status must be present, current, upcoming, or historical")
		return
	}
	for _, field := range []struct {
		name        string
		destination *string
	}{
		{name: "line_id", destination: &query.LineID},
		{name: "station_id", destination: &query.StationID},
		{name: "cause", destination: &query.Cause},
		{name: "effect", destination: &query.Effect},
	} {
		name, destination := field.name, field.destination
		value, present, err := scalar(values, name)
		if err != nil {
			h.invalidQuery(response, err.Error())
			return
		}
		if present {
			limit := 256
			if name == "cause" || name == "effect" {
				limit = 64
			}
			if err := validateString(name, value, limit, 0); err != nil {
				h.invalidQuery(response, err.Error())
				return
			}
			*destination = value
		}
	}
	for _, field := range []struct {
		name        string
		destination **time.Time
	}{
		{name: "from", destination: &query.From},
		{name: "to", destination: &query.To},
	} {
		name, destination := field.name, field.destination
		value, present, err := scalar(values, name)
		if err != nil {
			h.invalidQuery(response, err.Error())
			return
		}
		if present {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				h.invalidQuery(response, name+" must be an RFC3339 timestamp")
				return
			}
			parsed = parsed.UTC()
			*destination = &parsed
		}
	}
	if query.From != nil && query.To != nil && !query.From.Before(*query.To) {
		h.invalidQuery(response, "from must be before to")
		return
	}
	if query.Status == database.AlertStatusHistorical {
		query.Page, query.PageSize = 1, 25
		var err error
		if query.Page, err = positiveInt(values, "page", query.Page, maxHistoricalAlertPage); err != nil {
			h.invalidQuery(response, err.Error())
			return
		}
		if query.PageSize, err = positiveInt(values, "page_size", query.PageSize, 100); err != nil {
			h.invalidQuery(response, err.Error())
			return
		}
	} else if _, page := values["page"]; page {
		h.invalidQuery(response, "page is only supported for historical alerts")
		return
	} else if _, pageSize := values["page_size"]; pageSize {
		h.invalidQuery(response, "page_size is only supported for historical alerts")
		return
	}

	ctx, cancel := context.WithTimeout(request.Context(), h.requestTimeout)
	defer cancel()
	result, err := h.reads.ListAlerts(ctx, query)
	if err != nil {
		h.writeRepositoryError(response, request, "alert list query", err)
		return
	}
	var meta any = alertMeta{Count: len(result.Alerts), Status: query.Status}
	if query.Status == database.AlertStatusHistorical {
		meta = historicalAlertMeta{
			Count: len(result.Alerts), Status: query.Status, Total: result.Total,
			Page: query.Page, PageSize: query.PageSize,
			TotalPages: (result.Total + query.PageSize - 1) / query.PageSize,
		}
	}
	h.writeJSON(response, http.StatusOK, collectionResponse{Data: result.Alerts, Meta: meta})
}

func (h *Handler) handleAlert(response http.ResponseWriter, request *http.Request) {
	id, ok := alertID(request.PathValue("id"))
	if !ok {
		h.writeError(response, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if request.URL.RawQuery != "" {
		h.invalidQuery(response, "query parameters are not supported")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), h.requestTimeout)
	defer cancel()
	result, err := h.reads.GetAlert(ctx, id)
	if err != nil {
		h.writeRepositoryError(response, request, "alert detail query", err)
		return
	}
	h.writeJSON(response, http.StatusOK, dataResponse{Data: result})
}

func (h *Handler) handleAlertRevisions(response http.ResponseWriter, request *http.Request) {
	id, ok := alertID(request.PathValue("id"))
	if !ok {
		h.writeError(response, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if request.URL.RawQuery != "" {
		h.invalidQuery(response, "query parameters are not supported")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), h.requestTimeout)
	defer cancel()
	result, err := h.reads.ListAlertRevisions(ctx, id)
	if err != nil {
		h.writeRepositoryError(response, request, "alert revision query", err)
		return
	}
	h.writeJSON(response, http.StatusOK, collectionResponse{Data: result, Meta: collectionMeta{Count: len(result)}})
}

func (h *Handler) handleLines(response http.ResponseWriter, request *http.Request) {
	values, message := parseQuery(request, "include_replacement_bus")
	if message != "" {
		h.invalidQuery(response, message)
		return
	}
	include, err := boolean(values, "include_replacement_bus", false)
	if err != nil {
		h.invalidQuery(response, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), h.requestTimeout)
	defer cancel()
	result, err := h.reads.ListLines(ctx, include, h.now().UTC())
	if err != nil {
		h.writeRepositoryError(response, request, "line list query", err)
		return
	}
	h.writeJSON(response, http.StatusOK, collectionResponse{Data: result, Meta: collectionMeta{Count: len(result)}})
}

func (h *Handler) handleLine(response http.ResponseWriter, request *http.Request) {
	id, ok := resourceID(request.PathValue("id"))
	if !ok {
		h.invalidQuery(response, "line id is invalid")
		return
	}
	if request.URL.RawQuery != "" {
		h.invalidQuery(response, "query parameters are not supported")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), h.requestTimeout)
	defer cancel()
	result, err := h.reads.GetLine(ctx, id, h.now().UTC())
	if err != nil {
		h.writeRepositoryError(response, request, "line detail query", err)
		return
	}
	h.writeJSON(response, http.StatusOK, dataResponse{Data: result})
}

func (h *Handler) handleStations(response http.ResponseWriter, request *http.Request) {
	values, message := parseQuery(request, "q", "line_id")
	if message != "" {
		h.invalidQuery(response, message)
		return
	}
	query := database.StationQuery{}
	for _, field := range []struct {
		name        string
		destination *string
	}{
		{name: "q", destination: &query.Q},
		{name: "line_id", destination: &query.LineID},
	} {
		name, destination := field.name, field.destination
		value, present, err := scalar(values, name)
		if err != nil {
			h.invalidQuery(response, err.Error())
			return
		}
		if present {
			maxBytes, maxRunes := 256, 0
			if name == "q" {
				maxBytes, maxRunes = 0, 100
			}
			if err := validateString(name, value, maxBytes, maxRunes); err != nil {
				h.invalidQuery(response, err.Error())
				return
			}
			*destination = value
		}
	}
	ctx, cancel := context.WithTimeout(request.Context(), h.requestTimeout)
	defer cancel()
	result, err := h.reads.ListStations(ctx, query, h.now().UTC())
	if err != nil {
		h.writeRepositoryError(response, request, "station list query", err)
		return
	}
	h.writeJSON(response, http.StatusOK, collectionResponse{Data: result, Meta: collectionMeta{Count: len(result)}})
}

func (h *Handler) handleStation(response http.ResponseWriter, request *http.Request) {
	id, ok := resourceID(request.PathValue("id"))
	if !ok {
		h.invalidQuery(response, "station id is invalid")
		return
	}
	if request.URL.RawQuery != "" {
		h.invalidQuery(response, "query parameters are not supported")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), h.requestTimeout)
	defer cancel()
	result, err := h.reads.GetStation(ctx, id, h.now().UTC())
	if err != nil {
		h.writeRepositoryError(response, request, "station detail query", err)
		return
	}
	h.writeJSON(response, http.StatusOK, dataResponse{Data: result})
}

func (h *Handler) handleLineAnalytics(response http.ResponseWriter, request *http.Request) {
	query, message := h.analyticsQuery(request, true)
	if message != "" {
		h.invalidQuery(response, message)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), h.requestTimeout)
	defer cancel()
	result, err := h.reads.ListLineAnalytics(ctx, query)
	if err != nil {
		h.writeRepositoryError(response, request, "line analytics list query", err)
		return
	}
	h.writeJSON(response, http.StatusOK, analyticsResponse{Data: result, Meta: newAnalyticsMeta(query, len(result))})
}

func (h *Handler) handleLineAnalyticsDetail(response http.ResponseWriter, request *http.Request) {
	id, ok := resourceID(request.PathValue("id"))
	if !ok {
		h.invalidQuery(response, "line id is invalid")
		return
	}
	query, message := h.analyticsQuery(request, false)
	if message != "" {
		h.invalidQuery(response, message)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), h.requestTimeout)
	defer cancel()
	result, err := h.reads.GetLineAnalytics(ctx, id, query)
	if err != nil {
		h.writeRepositoryError(response, request, "line analytics detail query", err)
		return
	}
	h.writeJSON(response, http.StatusOK, analyticsResponse{Data: result, Meta: newAnalyticsMeta(query, 1)})
}

func (h *Handler) analyticsQuery(request *http.Request, collection bool) (database.AnalyticsQuery, string) {
	values, message := parseQuery(request, "from", "to", "interval", "include_replacement_bus")
	if message != "" {
		return database.AnalyticsQuery{}, message
	}
	if !collection {
		if _, present := values["include_replacement_bus"]; present {
			return database.AnalyticsQuery{}, "include_replacement_bus is not supported for line analytics detail"
		}
	}
	now := h.now().UTC()
	query := database.AnalyticsQuery{Now: now, From: now.Add(-30 * 24 * time.Hour), To: now, Interval: "day"}
	for _, field := range []struct {
		name        string
		destination *time.Time
	}{
		{name: "from", destination: &query.From},
		{name: "to", destination: &query.To},
	} {
		name, destination := field.name, field.destination
		value, present, err := scalar(values, name)
		if err != nil {
			return database.AnalyticsQuery{}, err.Error()
		}
		if present {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return database.AnalyticsQuery{}, name + " must be an RFC3339 timestamp"
			}
			*destination = parsed.UTC()
		}
	}
	if !query.From.Before(query.To) {
		return database.AnalyticsQuery{}, "from must be before to"
	}
	if query.To.Sub(query.From) > 366*24*time.Hour {
		return database.AnalyticsQuery{}, "analytics range must not exceed 366 days"
	}
	if value, present, err := scalar(values, "interval"); err != nil {
		return database.AnalyticsQuery{}, err.Error()
	} else if present {
		if value != "day" && value != "week" {
			return database.AnalyticsQuery{}, "interval must be day or week"
		}
		query.Interval = value
	}
	if collection {
		include, err := boolean(values, "include_replacement_bus", false)
		if err != nil {
			return database.AnalyticsQuery{}, err.Error()
		}
		query.IncludeReplacementBus = include
	}
	return query, ""
}

func newAnalyticsMeta(query database.AnalyticsQuery, count int) analyticsMeta {
	return analyticsMeta{
		Count: count, From: query.From, To: query.To, Interval: query.Interval,
		Timezone: "UTC", MetricBasis: "continuous_feed_observation_episodes",
	}
}

func parseQuery(request *http.Request, allowed ...string) (url.Values, string) {
	values, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		return nil, "query string is malformed"
	}
	allowedNames := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allowedNames[name] = struct{}{}
	}
	for name := range values {
		if _, ok := allowedNames[name]; !ok {
			return nil, fmt.Sprintf("unknown query parameter %q", name)
		}
	}
	return values, ""
}

func scalar(values url.Values, name string) (string, bool, error) {
	items, present := values[name]
	if !present {
		return "", false, nil
	}
	if len(items) != 1 {
		return "", false, fmt.Errorf("query parameter %q must be supplied once", name)
	}
	if items[0] == "" {
		return "", false, fmt.Errorf("query parameter %q must not be empty", name)
	}
	return items[0], true, nil
}

func positiveInt(values url.Values, name string, defaultValue, maximum int) (int, error) {
	value, present, err := scalar(values, name)
	if err != nil {
		return 0, err
	}
	if !present {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	if maximum > 0 && parsed > maximum {
		return 0, fmt.Errorf("%s must not exceed %d", name, maximum)
	}
	return parsed, nil
}

func boolean(values url.Values, name string, defaultValue bool) (bool, error) {
	value, present, err := scalar(values, name)
	if err != nil {
		return false, err
	}
	if !present {
		return defaultValue, nil
	}
	if value == "true" {
		return true, nil
	}
	if value == "false" {
		return false, nil
	}
	return false, fmt.Errorf("%s must be true or false", name)
}

func validateString(name, value string, maxBytes, maxRunes int) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", name)
	}
	if maxBytes > 0 && len(value) > maxBytes {
		return fmt.Errorf("%s must not exceed %d bytes", name, maxBytes)
	}
	if maxRunes > 0 && utf8.RuneCountInString(value) > maxRunes {
		return fmt.Errorf("%s must not exceed %d characters", name, maxRunes)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("%s must not contain control characters", name)
		}
	}
	return nil
}

func alertID(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, false
		}
	}
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil && id > 0
}

func resourceID(value string) (string, bool) {
	return value, validateString("id", value, 256, 0) == nil && value != ""
}

func (h *Handler) writeRepositoryError(response http.ResponseWriter, request *http.Request, operation string, err error) {
	if errors.Is(err, database.ErrNotFound) {
		h.writeError(response, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		h.logger.WarnContext(request.Context(), operation+" timed out", "error", err)
		h.writeError(response, http.StatusGatewayTimeout, "request_timeout", "request timed out")
		return
	}
	h.logger.ErrorContext(request.Context(), operation+" failed", "error", err)
	h.writeError(response, http.StatusInternalServerError, "internal_error", "internal server error")
}

func (h *Handler) invalidQuery(response http.ResponseWriter, message string) {
	h.writeError(response, http.StatusBadRequest, "invalid_query", message)
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
