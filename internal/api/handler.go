package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/favxlaw/distributed-workflow-engine-go/internal/events"
	"github.com/favxlaw/distributed-workflow-engine-go/internal/storage"
	"github.com/favxlaw/distributed-workflow-engine-go/internal/workflow"
)

type Handler struct {
	store  *storage.DynamoStore
	events *events.EventStore
}

func NewHandler(store *storage.DynamoStore, events *events.EventStore) *Handler {
	return &Handler{store: store, events: events}
}

type createOrderRequest struct {
	ID string `json:"id"`
}

type transitionOrderRequest struct {
	EventID  string `json:"event_id"`
	NewState string `json:"new_state"`
}

type orderResponse struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Version int    `json:"version"`
}

func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	order := &workflow.Order{ID: req.ID, State: workflow.Created}
	if err := h.store.SaveOrder(r.Context(), order); err != nil {
		var existErr storage.ErrOrderAlreadyExists
		if errors.As(err, &existErr) {
			writeError(w, http.StatusConflict, "order already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, orderResponse{ID: order.ID, State: string(order.State), Version: order.Version})
}

func (h *Handler) TransitionOrder(w http.ResponseWriter, r *http.Request) {
	orderID := r.PathValue("id")
	if orderID == "" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var req transitionOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.EventID == "" || req.NewState == "" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	processed, err := h.events.IsProcessed(r.Context(), req.EventID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if processed {
		writeJSON(w, http.StatusOK, map[string]string{"message": "event already processed"})
		return
	}

	order, err := h.store.GetOrder(r.Context(), orderID)
	if err != nil {
		var notFoundErr storage.ErrOrderNotFound
		if errors.As(err, &notFoundErr) {
			writeError(w, http.StatusNotFound, "order not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	newState := workflow.OrderState(req.NewState)
	if err := h.store.TransitionOrder(r.Context(), order, newState); err != nil {
		var notFoundErr storage.ErrOrderNotFound
		var versionErr storage.ErrVersionConflict
		var invalidTransErr workflow.ErrInvalidTransition
		if errors.As(err, &notFoundErr) {
			writeError(w, http.StatusNotFound, "order not found")
			return
		}
		if errors.As(err, &versionErr) {
			writeError(w, http.StatusConflict, "version conflict, retry")
			return
		}
		if errors.As(err, &invalidTransErr) {
			writeError(w, http.StatusUnprocessableEntity, "invalid state transition")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := h.events.MarkProcessed(r.Context(), req.EventID); err != nil {
		var dupErr events.ErrDuplicateEvent
		if errors.As(err, &dupErr) {
			writeJSON(w, http.StatusOK, map[string]string{"message": "event already processed"})
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, orderResponse{ID: order.ID, State: string(order.State), Version: order.Version})
}
