package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"main/internal/config"
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
