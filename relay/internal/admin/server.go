// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package admin serves the local management API and optional HTTP ingest endpoints.
package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/laststate/relay/internal/ingest"
	"github.com/laststate/relay/internal/store"
	"github.com/laststate/relay/internal/traceapi"
)

type Server struct {
	Store         *store.Store
	Ingest        ingest.Service
	AdminToken    string
	IngestToken   string
	SourceID      string
	MaxBodyBytes  int64
	MaxConcurrent int
	SpeedLimit    RateLimitConfig
	semaphore     chan struct{}
}

type Capabilities struct {
	APIVersion     string   `json:"api_version"`
	LEPVersions    []int    `json:"lep_versions"`
	MaxEventSize   int64    `json:"max_event_size"`
	MaxBatchEvents int      `json:"max_batch_events"`
	MaxBatchBytes  int64    `json:"max_batch_bytes"`
	Compression    []string `json:"compression"`
	ArtifactUpload bool     `json:"artifact_upload"`
	BatchIngest    bool     `json:"batch_ingest"`
	BinaryBatch    bool     `json:"binary_batch"`
	Authentication []string `json:"authentication"`
}

// AdminHandler exposes only privileged local management operations.
func (server *Server) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", server.status)
	mux.HandleFunc("GET /v1/health", server.health)
	mux.HandleFunc("GET /v1/ready", server.ready)
	mux.HandleFunc("GET /v1/events", server.events)
	mux.HandleFunc("GET /v1/events/{id}", server.event)
	mux.HandleFunc("POST /v1/events/{id}/replay", server.replay)
	mux.HandleFunc("GET /v1/destinations", server.destinations)
	mux.HandleFunc("POST /v1/destinations/{id}/pause", server.pause)
	mux.HandleFunc("POST /v1/destinations/{id}/resume", server.resume)
	mux.HandleFunc("POST /v1/spool/prune", server.prune)
	mux.HandleFunc("POST /v1/spool/reconcile", server.reconcile)
	// Health endpoints accessible without auth.
	policies := EndpointAuth{
		"/v1/health": AuthNone,
		"/v1/ready":  AuthNone,
	}
	// All other admin routes require admin token.
	return secureHeaders(withEndpointAuth(server, policies, AuthAdmin)(mux))
}

// IngestHandler does not mount admin routes.
func (server *Server) IngestHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/ingest/capabilities", server.capabilities)
	mux.HandleFunc("GET /v1/relay/capabilities", server.capabilities)
	mux.HandleFunc("POST /v1/ingest", server.ingest)
	mux.HandleFunc("POST /v1/events:batch", server.ingestBatch)
	handler := secureHeaders(withEndpointAuth(server, EndpointAuth{
		"/v1/ingest/capabilities": AuthNone,
		"/v1/relay/capabilities":  AuthNone,
	}, AuthIngest)(server.limitConcurrency(mux)))
	if server.SpeedLimit.RequestsPerSecond > 0 {
		handler = RateLimitMiddleware(NewRateLimiter(server.SpeedLimit))(handler)
	}
	return handler
}

// Handler is retained for source compatibility but now returns admin-only API.
func (server *Server) Handler() http.Handler { return server.AdminHandler() }

func (server *Server) limitConcurrency(next http.Handler) http.Handler {
	if server.MaxConcurrent <= 0 {
		server.MaxConcurrent = 32
	}
	if server.semaphore == nil {
		server.semaphore = make(chan struct{}, server.MaxConcurrent)
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		select {
		case server.semaphore <- struct{}{}:
			defer func() { <-server.semaphore }()
			next.ServeHTTP(writer, request)
		default:
			writer.Header().Set("Retry-After", "1")
			writeError(writer, http.StatusServiceUnavailable, "busy", "ingest concurrency limit reached")
		}
	})
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(writer, request)
	})
}

func (server *Server) status(writer http.ResponseWriter, request *http.Request) {
	status, err := server.Store.Status(request.Context())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, status)
}

func (server *Server) health(writer http.ResponseWriter, request *http.Request) {
	if _, err := server.Store.Status(request.Context()); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "unhealthy", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "healthy"})
}

func (server *Server) ready(writer http.ResponseWriter, request *http.Request) {
	status, err := server.Store.Status(request.Context())
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "not_ready", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"status": "ready", "free_bytes": status.FreeBytes})
}

func (server *Server) events(writer http.ResponseWriter, request *http.Request) {
	offset, limit := parsePagination(request)
	events, err := server.Store.List(request.Context(), limit, offset)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"events": events,
		"total":  len(events),
		"offset": offset,
		"limit":  limit,
	})
}

func (server *Server) event(writer http.ResponseWriter, request *http.Request) {
	raw, err := server.Store.RawEvent(request.Context(), request.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(writer, http.StatusNotFound, "not_found", "event not found")
		} else {
			writeError(writer, http.StatusInternalServerError, "store_error", err.Error())
		}
		return
	}
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(raw)
}

func (server *Server) replay(writer http.ResponseWriter, request *http.Request) {
	if err := server.Store.Replay(request.Context(), request.PathValue("id"), request.URL.Query().Get("destination")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(writer, http.StatusNotFound, "not_found", "no matching delivery")
		} else {
			writeError(writer, http.StatusInternalServerError, "store_error", err.Error())
		}
		return
	}
	writer.WriteHeader(http.StatusAccepted)
}

func (server *Server) destinations(writer http.ResponseWriter, request *http.Request) {
	items, err := server.Store.DestinationStatuses(request.Context())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	offset, limit := parsePagination(request)
	_ = offset
	_ = limit
	writeJSON(writer, http.StatusOK, map[string]any{
		"destinations": items,
		"total":        len(items),
		"offset":       0,
		"limit":        limit,
	})
}

func (server *Server) pause(writer http.ResponseWriter, request *http.Request) {
	server.setPause(writer, request, true)
}
func (server *Server) resume(writer http.ResponseWriter, request *http.Request) {
	server.setPause(writer, request, false)
}

func (server *Server) setPause(writer http.ResponseWriter, request *http.Request, paused bool) {
	if err := server.Store.PauseDestination(request.Context(), request.PathValue("id"), paused); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(writer, http.StatusNotFound, "not_found", err.Error())
		} else {
			writeError(writer, http.StatusInternalServerError, "store_error", err.Error())
		}
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) prune(writer http.ResponseWriter, request *http.Request) {
	before := time.Now().UTC().Add(-30 * 24 * time.Hour)
	if raw := request.URL.Query().Get("before"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(writer, http.StatusBadRequest, "invalid_before", "before must be RFC3339")
			return
		}
		before = parsed
	}
	result, err := server.Store.Prune(request.Context(), before, true)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) reconcile(writer http.ResponseWriter, request *http.Request) {
	result, err := server.Store.Reconcile(request.Context())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) capabilities(writer http.ResponseWriter, request *http.Request) {
	max := server.MaxBodyBytes
	if max <= 0 {
		max = 4 << 20
	}
	maxBatch := max * 100
	if maxBatch > 64<<20 {
		maxBatch = 64 << 20
	}
	writeJSON(writer, http.StatusOK, Capabilities{
		APIVersion: "1", LEPVersions: []int{1, 2}, MaxEventSize: max,
		MaxBatchEvents: 100, MaxBatchBytes: maxBatch, Compression: []string{"identity", "zstd"},
		ArtifactUpload: false, BatchIngest: true, BinaryBatch: true, Authentication: []string{"none", "bearer"},
	})
}

func (server *Server) ingest(writer http.ResponseWriter, request *http.Request) {
	if contentType := request.Header.Get("Content-Type"); contentType != "" && !strings.HasPrefix(contentType, "application/octet-stream") {
		writeError(writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "expected application/octet-stream")
		return
	}
	max := server.MaxBodyBytes
	if max <= 0 {
		max = 4 << 20
	}
	body := http.MaxBytesReader(writer, request.Body, max)
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_body", "request body could not be read")
		return
	}
	source := server.SourceID
	if source == "" {
		source = "http"
	}
	result, err := server.Ingest.Accept(request.Context(), source, raw)
	if err != nil {
		switch ingest.Code(err) {
		case ingest.CodeUnsupported:
			writeError(writer, http.StatusUnprocessableEntity, "unsupported_protocol", err.Error())
		case ingest.CodeTooLarge:
			writeError(writer, http.StatusRequestEntityTooLarge, "too_large", err.Error())
		case ingest.CodeBusy:
			writer.Header().Set("Retry-After", "5")
			writeError(writer, http.StatusServiceUnavailable, "busy", err.Error())
		case ingest.CodeCorrupt:
			writeError(writer, http.StatusUnprocessableEntity, "corrupt_event", err.Error())
		default:
			writeError(writer, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	writer.Header().Set("X-Last-State-Event-ID", result.Event.ID)
	if result.Duplicate {
		writeJSON(writer, http.StatusOK, map[string]any{"id": result.Event.ID, "duplicate": true})
	} else {
		writeJSON(writer, http.StatusCreated, map[string]any{"id": result.Event.ID, "duplicate": false})
	}
}

func (server *Server) ingestBatch(writer http.ResponseWriter, request *http.Request) {
	maxEvent := server.MaxBodyBytes
	if maxEvent <= 0 {
		maxEvent = 4 << 20
	}
	maxBody := maxEvent * 100
	if maxBody > 64<<20 {
		maxBody = 64 << 20
	}
	body := http.MaxBytesReader(writer, request.Body, maxBody)
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_batch", "request body could not be read")
		return
	}
	if strings.EqualFold(request.Header.Get("Content-Encoding"), "zstd") {
		decoder, zerr := zstd.NewReader(nil)
		if zerr != nil {
			writeError(writer, http.StatusBadRequest, "invalid_batch", zerr.Error())
			return
		}
		decompressed, zerr := decoder.DecodeAll(raw, nil)
		decoder.Close()
		if zerr != nil {
			writeError(writer, http.StatusBadRequest, "invalid_batch", "zstd decompress failed")
			return
		}
		raw = decompressed
	}
	var batch traceapi.BatchRequest
	contentType := request.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/vnd.laststate.batch.v1") {
		events, berr := traceapi.DecodeBinaryBatch(raw)
		if berr != nil {
			writeError(writer, http.StatusBadRequest, "invalid_batch", berr.Error())
			return
		}
		batch.Events = events
	} else if err := json.Unmarshal(raw, &batch); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_batch", err.Error())
		return
	}
	if len(batch.Events) == 0 || len(batch.Events) > 100 {
		writeError(writer, http.StatusBadRequest, "invalid_batch", "events must contain between 1 and 100 items")
		return
	}
	source := server.SourceID
	if source == "" {
		source = "http-batch"
	}
	response := traceapi.BatchResponse{}
	for _, event := range batch.Events {
		id := event.EventID
		if id == "" {
			id = "unknown"
		}
		if int64(len(event.Payload)) > maxEvent {
			response.Rejected = append(response.Rejected, traceapi.RejectedEvent{EventID: id, Code: "too_large", Message: "event exceeds destination limit", Retryable: false})
			continue
		}
		result, err := server.Ingest.Accept(request.Context(), source, event.Payload)
		if err != nil {
			code := string(ingest.Code(err))
			retryable := ingest.Code(err) == ingest.CodeBusy || ingest.Code(err) == ingest.CodeInternal
			response.Rejected = append(response.Rejected, traceapi.RejectedEvent{EventID: id, Code: code, Message: err.Error(), Retryable: retryable})
			continue
		}
		accepted := traceapi.AcceptedEvent{EventID: id, Receipt: result.Event.ID}
		if result.Duplicate {
			response.Duplicates = append(response.Duplicates, accepted)
		} else {
			response.Accepted = append(response.Accepted, accepted)
		}
	}
	writeJSON(writer, http.StatusOK, response)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// parsePagination extracts offset and limit from query parameters, applying
// sensible defaults and hard caps to prevent runaway queries.
func parsePagination(request *http.Request) (offset, limit int) {
	offset = 0
	limit = 100
	const maxLimit = 500
	if raw := request.URL.Query().Get("limit"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 && value <= maxLimit {
			limit = value
		}
	}
	if raw := request.URL.Query().Get("offset"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value >= 0 {
			offset = value
		}
	}
	return offset, limit
}
