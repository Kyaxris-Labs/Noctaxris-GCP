package restlab

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
)

func TestWrapUnauthenticated(t *testing.T) {
	h := Wrap(func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{}, false
	}, func(http.ResponseWriter, *http.Request, authn.Principal) {
		t.Fatal("handler must not run")
	})
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want %d", rec.Code, http.StatusUnauthorized)
	}
	var body gcperrors.ErrorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Status != gcperrors.StatusUnauthenticated {
		t.Fatalf("status=%q", body.Error.Status)
	}
}

func TestWrapAuthenticated(t *testing.T) {
	called := false
	h := Wrap(func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "a@example.com", IsRoot: true}, true
	}, func(w http.ResponseWriter, r *http.Request, p authn.Principal) {
		called = true
		if p.Email != "a@example.com" {
			t.Fatalf("email=%q", p.Email)
		}
		WriteJSON(w, http.StatusOK, map[string]string{"ok": "1"})
	})
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !called {
		t.Fatal("handler not called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestRequireRoot(t *testing.T) {
	p := authn.Principal{Email: "root@lab", IsRoot: true}
	if err := Require(nil, p, "file.instances.get", "p1"); err != nil {
		t.Fatalf("root: %v", err)
	}
}

func TestRequireDenied(t *testing.T) {
	eval := &authz.Evaluator{}
	p := authn.Principal{Email: "user@lab", IsRoot: false}
	err := Require(eval, p, "file.instances.get", "p1")
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("err=%v want ErrDenied", err)
	}
}

func TestWriteAuthzErr(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteAuthzErr(rec, ErrDenied)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied status=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	WriteAuthzErr(rec, errors.New("boom"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("internal status=%d", rec.Code)
	}
}
