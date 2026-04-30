package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"main/internal/config"
	"main/internal/token"
)

func newTestServer(t *testing.T, publicAccountStatus bool) *Server {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	store := config.NewStore(path)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(cfg *config.Config) error {
		cfg.APIKey = "test-key"
		cfg.PublicAccountStatus = publicAccountStatus
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestHandleIndexRedirectsWithoutWebSession(t *testing.T) {
	s := newTestServer(t, false)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	s.handleIndex(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if location := w.Header().Get("Location"); location != "/login" {
		t.Fatalf("expected /login redirect, got %q", location)
	}
}

func TestHandleIndexAllowsValidWebSession(t *testing.T) {
	s := newTestServer(t, false)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "geminiweb2api_session", Value: "test-key"})
	w := httptest.NewRecorder()

	s.handleIndex(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleWebLoginSetsHttpOnlyCookie(t *testing.T) {
	s := newTestServer(t, false)
	req := httptest.NewRequest(http.MethodPost, "/api/web/login", strings.NewReader(`{"api_key":"test-key"}`))
	w := httptest.NewRecorder()

	s.handleWebLogin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != "geminiweb2api_session" || !cookies[0].HttpOnly {
		t.Fatalf("expected httponly session cookie, got %#v", cookies)
	}
}

func TestHandleAccountsRequiresAuthByDefault(t *testing.T) {
	s := newTestServer(t, false)
	req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	w := httptest.NewRecorder()

	s.handleAccounts(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleAccountsAllowsPublicStatusWhenConfigured(t *testing.T) {
	s := newTestServer(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	w := httptest.NewRecorder()

	s.handleAccounts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleModelsUses3SeriesDefaults(t *testing.T) {
	s := newTestServer(t, false)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()

	s.handleModels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal models response: %v", err)
	}

	got := make([]string, 0, len(resp.Data))
	for _, model := range resp.Data {
		got = append(got, model.ID)
	}
	expected := []string{"gemini-3-pro", "gemini-3-flash"}
	if len(got) != len(expected) {
		t.Fatalf("expected %d models, got %d: %v", len(expected), len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("expected models %v, got %v", expected, got)
		}
	}
}

func TestPersistentStateRestoresAccountTokenSnapshots(t *testing.T) {
	s := newTestServer(t, false)

	s.tokenManager.RestoreTokenSnapshots(map[string]token.AccountTokenSnapshot{
		"__default__": {
			SNlM0e:    "persisted-token",
			BLToken:   "persisted-bl",
			FSID:      "persisted-fsid",
			ReqID:     12345,
			FetchedAt: time.Now().UTC().Truncate(time.Second),
		},
	})
	s.savePersistentState()

	reloaded, err := New(s.configStore)
	if err != nil {
		t.Fatal(err)
	}

	accounts := reloaded.tokenManager.TokenSnapshots()
	snapshot, ok := accounts["__default__"]
	if !ok {
		t.Fatal("expected persisted token snapshot to load")
	}
	if snapshot.SNlM0e != "persisted-token" || snapshot.BLToken != "persisted-bl" || snapshot.FSID != "persisted-fsid" {
		t.Fatalf("unexpected restored snapshot: %+v", snapshot)
	}
	if snapshot.ReqID != 12345 {
		t.Fatalf("expected req id 12345, got %d", snapshot.ReqID)
	}
}
