package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/auth"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/clock"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/httpapi"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/service"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/store"
)

type apiFixture struct {
	server   *httptest.Server
	database *store.Store
	password string
}

func newAPIFixture(t *testing.T) apiFixture {
	t.Helper()
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	password := "http-password-2026"
	hash, err := auth.PasswordHash(password)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []identity.User{
		{Username: "manager", DisplayName: "产线负责人", Role: identity.RoleLineManager, Active: true, PasswordHash: hash},
		{Username: "operator", DisplayName: "现场操作员", Role: identity.RoleOperator, Active: true, PasswordHash: hash},
	} {
		if _, err = database.CreateUser(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	manual := clock.NewManual(time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC))
	authService := auth.New(database, manual, time.Hour)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := httpapi.New(database, authService, service.NewLifecycle(database, manual), service.NewScheduling(database, manual), service.NewMaintenance(database, manual), logger)
	server := httptest.NewServer(handler)
	t.Cleanup(func() { server.Close(); _ = database.Close() })
	return apiFixture{server: server, database: database, password: password}
}

func request(t *testing.T, method, url, token string, body any) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return response, data
}

func login(t *testing.T, fx apiFixture, username string) string {
	t.Helper()
	response, data := request(t, http.MethodPost, fx.server.URL+"/v1/auth/login", "", map[string]string{"username": username, "password": fx.password})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d body=%s", response.StatusCode, data)
	}
	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Token == "" {
		t.Fatal("login returned empty token")
	}
	return result.Token
}

func TestHealthAndReadyArePublicAndReturnRequestID(t *testing.T) {
	fx := newAPIFixture(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		response, data := request(t, http.MethodGet, fx.server.URL+path, "", nil)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.StatusCode, data)
		}
		if response.Header.Get("X-Request-ID") == "" {
			t.Fatalf("%s did not return request id", path)
		}
		if !strings.Contains(string(data), "status") {
			t.Fatalf("%s body=%s", path, data)
		}
	}
}

func TestProtectedRouteRequiresBearerToken(t *testing.T) {
	fx := newAPIFixture(t)
	response, data := request(t, http.MethodGet, fx.server.URL+"/v1/cells", "", nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.StatusCode, data)
	}
	if !strings.Contains(string(data), `"code":"UNAUTHENTICATED"`) || !strings.Contains(string(data), `"request_id"`) {
		t.Fatalf("unexpected error body: %s", data)
	}
}

func TestLoginRejectsUnknownJSONFields(t *testing.T) {
	fx := newAPIFixture(t)
	response, data := request(t, http.MethodPost, fx.server.URL+"/v1/auth/login", "", map[string]any{"username": "manager", "password": fx.password, "admin": true})
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", response.StatusCode, data)
	}
	if !strings.Contains(string(data), "INVALID_ARGUMENT") {
		t.Fatalf("unexpected error body: %s", data)
	}
}

func TestLoginAndLogoutRevokeToken(t *testing.T) {
	fx := newAPIFixture(t)
	token := login(t, fx, "operator")
	response, data := request(t, http.MethodGet, fx.server.URL+"/v1/cells?page=1&page_size=10", token, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authorized list status=%d body=%s", response.StatusCode, data)
	}
	response, data = request(t, http.MethodPost, fx.server.URL+"/v1/auth/logout", token, nil)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", response.StatusCode, data)
	}
	response, data = request(t, http.MethodGet, fx.server.URL+"/v1/cells", token, nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token status=%d body=%s", response.StatusCode, data)
	}
}

func TestRoleAuthorizationReturnsStableForbiddenError(t *testing.T) {
	fx := newAPIFixture(t)
	token := login(t, fx, "operator")
	response, data := request(t, http.MethodPost, fx.server.URL+"/v1/cells", token, map[string]any{"code": "RC-HTTP", "name": "HTTP 机器人", "workstation_id": 1, "integrator_id": 1})
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.StatusCode, data)
	}
	if !strings.Contains(string(data), `"code":"FORBIDDEN"`) || !strings.Contains(string(data), `"message":"you cannot perform this action"`) {
		t.Fatalf("unexpected forbidden response: %s", data)
	}
}

func TestRequestIDIsAcceptedWhenValidAndReplacedWhenOversized(t *testing.T) {
	fx := newAPIFixture(t)
	req, err := http.NewRequest(http.MethodGet, fx.server.URL+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Request-ID", "caller-request-id")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.Header.Get("X-Request-ID") != "caller-request-id" {
		t.Fatalf("request id = %q", response.Header.Get("X-Request-ID"))
	}
	req, _ = http.NewRequest(http.MethodGet, fx.server.URL+"/healthz", nil)
	req.Header.Set("X-Request-ID", strings.Repeat("x", 129))
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if got := response.Header.Get("X-Request-ID"); got == "" || got == strings.Repeat("x", 129) {
		t.Fatalf("oversized request id was not replaced: %q", got)
	}
}
