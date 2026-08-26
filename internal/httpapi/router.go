package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/auth"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/lifecycle"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/maintenance"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/schedule"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/service"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/store"
)

type API struct {
	store       *store.Store
	auth        *auth.Service
	lifecycle   *service.Lifecycle
	scheduling  *service.Scheduling
	maintenance *service.Maintenance
	logger      *slog.Logger
}

func New(s *store.Store, a *auth.Service, l *service.Lifecycle, sc *service.Scheduling, m *service.Maintenance, logger *slog.Logger) http.Handler {
	api := &API{store: s, auth: a, lifecycle: l, scheduling: sc, maintenance: m, logger: logger}
	middleware := NewMiddleware(a, logger)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", api.health)
	mux.HandleFunc("GET /readyz", api.ready)
	mux.HandleFunc("POST /v1/auth/login", api.login)
	secured := http.NewServeMux()
	secured.HandleFunc("POST /v1/auth/logout", api.logout)
	secured.HandleFunc("GET /v1/cells", api.listCells)
	secured.HandleFunc("POST /v1/cells", api.createCell)
	secured.HandleFunc("POST /v1/cells/{id}/transition", api.transitionCell)
	secured.HandleFunc("POST /v1/cells/{id}/inspections", api.inspectCell)
	secured.HandleFunc("POST /v1/cells/{id}/calibration-failures", api.calibrationFailure)
	secured.HandleFunc("POST /v1/windows", api.createWindow)
	secured.HandleFunc("POST /v1/windows/{id}/approve", api.approveWindow)
	secured.HandleFunc("POST /v1/windows/{id}/cancel", api.cancelWindow)
	secured.HandleFunc("POST /v1/maintenance", api.createMaintenance)
	secured.HandleFunc("POST /v1/maintenance/{id}/transition", api.transitionMaintenance)
	secured.HandleFunc("GET /v1/audit/{type}/{id}", api.listAudit)
	mux.Handle("/v1/", middleware.Authenticate(secured))
	return middleware.Base(mux)
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "alive"})
}
func (a *API) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.store.Ready(ctx); err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ready"})
}
func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	result, err := a.auth.Login(r.Context(), input.Username, input.Password)
	if err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	writeJSON(w, 200, result)
}
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	principal, err := service.PrincipalFromContext(r.Context())
	if err == nil {
		err = a.auth.Logout(r.Context(), principal)
	}
	if err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listCells(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	result, err := a.lifecycle.List(r.Context(), lifecycle.CellStatus(r.URL.Query().Get("status")), page, size)
	if err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	writeJSON(w, 200, result)
}

func (a *API) createCell(w http.ResponseWriter, r *http.Request) {
	var input lifecycle.RobotCell
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	result, err := a.lifecycle.CreateCell(r.Context(), input, RequestID(r.Context()))
	if err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	writeJSON(w, 201, result)
}

func (a *API) transitionCell(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	var input struct {
		Version int64                `json:"version"`
		Status  lifecycle.CellStatus `json:"status"`
		Reason  string               `json:"reason"`
	}
	if err = decodeJSON(w, r, &input); err == nil {
		var result lifecycle.RobotCell
		result, err = a.lifecycle.Transition(r.Context(), id, input.Version, input.Status, input.Reason, RequestID(r.Context()))
		if err == nil {
			writeJSON(w, 200, result)
			return
		}
	}
	writeError(a.logger, w, r, err)
}

func (a *API) inspectCell(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	var input lifecycle.Inspection
	if err = decodeJSON(w, r, &input); err == nil {
		input.CellID = id
		var result lifecycle.RobotCell
		result, err = a.lifecycle.RecordInspection(r.Context(), input, RequestID(r.Context()))
		if err == nil {
			writeJSON(w, 201, result)
			return
		}
	}
	writeError(a.logger, w, r, err)
}

func (a *API) calibrationFailure(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if err = decodeJSON(w, r, &input); err == nil {
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		job, jobErr := a.lifecycle.ReportCalibrationFailure(r.Context(), id, key, input.Reason, RequestID(r.Context()))
		err = jobErr
		if err == nil {
			writeJSON(w, 202, job)
			return
		}
	}
	writeError(a.logger, w, r, err)
}

func (a *API) createWindow(w http.ResponseWriter, r *http.Request) {
	var input schedule.WorkWindow
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	result, err := a.scheduling.Request(r.Context(), input, RequestID(r.Context()))
	if err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	writeJSON(w, 201, result)
}

func (a *API) approveWindow(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	var input struct {
		Version       int64  `json:"version"`
		Qualification string `json:"qualification"`
	}
	if err = decodeJSON(w, r, &input); err == nil {
		var result schedule.WorkWindow
		result, err = a.scheduling.Approve(r.Context(), id, input.Version, input.Qualification, RequestID(r.Context()))
		if err == nil {
			writeJSON(w, 200, result)
			return
		}
	}
	writeError(a.logger, w, r, err)
}

func (a *API) cancelWindow(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	var input struct {
		Version int64  `json:"version"`
		Reason  string `json:"reason"`
	}
	if err = decodeJSON(w, r, &input); err == nil {
		var result schedule.WorkWindow
		result, err = a.scheduling.Cancel(r.Context(), id, input.Version, input.Reason, RequestID(r.Context()))
		if err == nil {
			writeJSON(w, 200, result)
			return
		}
	}
	writeError(a.logger, w, r, err)
}

func (a *API) createMaintenance(w http.ResponseWriter, r *http.Request) {
	var input maintenance.Order
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	result, err := a.maintenance.Open(r.Context(), input, RequestID(r.Context()))
	if err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	writeJSON(w, 201, result)
}

func (a *API) transitionMaintenance(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	var input struct {
		Version int64              `json:"version"`
		Status  maintenance.Status `json:"status"`
	}
	if err = decodeJSON(w, r, &input); err == nil {
		var result maintenance.Order
		result, err = a.maintenance.Advance(r.Context(), id, input.Version, input.Status, RequestID(r.Context()))
		if err == nil {
			writeJSON(w, 200, result)
			return
		}
	}
	writeError(a.logger, w, r, err)
}

func (a *API) listAudit(w http.ResponseWriter, r *http.Request) {
	events, err := a.store.ListAudit(r.Context(), r.PathValue("type"), r.PathValue("id"), 100)
	if err != nil {
		writeError(a.logger, w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": events})
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.New(apperr.ErrInvalid, "http.path_id", "path id must be a positive integer")
	}
	return id, nil
}
