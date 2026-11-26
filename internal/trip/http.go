package trip

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	api "github.com/reybug/TripGo/internal/generated"
)

type Handler struct {
	service *Service
	pool    *pgxpool.Pool
	timeout time.Duration
	schema  *openapi3.Schema
	logger  *slog.Logger
}

func NewHandler(service *Service, pool *pgxpool.Pool, timeout time.Duration, logger *slog.Logger) (*Handler, error) {
	spec, err := api.GetSwagger()
	if err != nil {
		return nil, err
	}
	return &Handler{service: service, pool: pool, timeout: timeout, schema: spec.Components.Schemas["TripData"].Value, logger: logger}, nil
}

func respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (h *Handler) problem(w http.ResponseWriter, r *http.Request, status int, code, title, detail string) {
	instance := r.URL.Path
	p := api.Problem{Type: "https://tripgo.example/problems/" + strings.ReplaceAll(code, "_", "-"), Title: title, Status: int32(status), Code: code, Detail: &detail, Instance: &instance}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

func (h *Handler) failure(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrDriverBusy):
		h.problem(w, r, 409, "driver_busy", "Driver busy", "Driver already has an active trip")
	case errors.Is(err, ErrNotFound):
		h.problem(w, r, 404, "trip_not_found", "Trip not found", "Trip was not found")
	case errors.Is(err, ErrCompleted):
		h.problem(w, r, 409, "trip_completed", "Trip completed", "Trip is already completed")
	default:
		h.logger.ErrorContext(r.Context(), "request failed", "error", err)
		h.problem(w, r, 500, "internal_error", "Internal error", "Internal server error")
	}
}

func (h *Handler) InvalidRequest(w http.ResponseWriter, r *http.Request, _ error) {
	h.problem(w, r, 400, "invalid_request", "Invalid request", "Request validation failed")
}

func (h *Handler) CreateTrip(w http.ResponseWriter, r *http.Request, _ api.CreateTripParams) {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		h.InvalidRequest(w, r, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		h.InvalidRequest(w, r, err)
		return
	}
	var value any
	if err = json.Unmarshal(raw, &value); err != nil {
		h.InvalidRequest(w, r, err)
		return
	}
	if err = h.schema.VisitJSON(value); err != nil {
		h.InvalidRequest(w, r, err)
		return
	}
	var data api.TripData
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&data); err != nil || data.UserId == uuid.Nil || data.DriverId == uuid.Nil {
		h.InvalidRequest(w, r, err)
		return
	}
	t, err := h.service.Create(r.Context(), data)
	if err != nil {
		h.failure(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/trips/"+t.Id.String())
	respond(w, http.StatusCreated, t)
}

func (h *Handler) GetTrip(w http.ResponseWriter, r *http.Request, id api.TripId) {
	t, err := h.service.Get(r.Context(), id)
	if err != nil {
		h.failure(w, r, err)
		return
	}
	respond(w, http.StatusOK, t)
}

func (h *Handler) FinishTrip(w http.ResponseWriter, r *http.Request, id api.TripId) {
	t, err := h.service.Finish(r.Context(), id)
	if err != nil {
		h.failure(w, r, err)
		return
	}
	respond(w, http.StatusOK, t)
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	respond(w, 200, api.HealthResponse{Status: api.Ok})
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	if err := h.pool.Ping(ctx); err != nil {
		respond(w, 503, api.HealthResponse{Status: api.Unavailable})
		return
	}
	respond(w, 200, api.HealthResponse{Status: api.Ok})
}
