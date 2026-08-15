package restlab_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
)

func TestLocationOperationGetHooks(t *testing.T) {
	restlab.ClearLocationOperationGetHooks()
	t.Cleanup(restlab.ClearLocationOperationGetHooks)

	called := false
	restlab.RegisterLocationOperationGetHook(func(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location, opID string) bool {
		if project == "p" && location == "us" && opID == "op1" {
			called = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"name":"op1","done":true}`))
			return true
		}
		return false
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ok := restlab.DispatchLocationOperationGetHooks(rec, req, authn.Principal{Email: "a@b.c", IsRoot: true}, "p", "us", "op1")
	if !ok || !called || rec.Code != http.StatusOK {
		t.Fatalf("ok=%v called=%v code=%d body=%s", ok, called, rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	ok = restlab.DispatchLocationOperationGetHooks(rec, req, authn.Principal{}, "p", "us", "missing")
	if ok {
		t.Fatal("expected no hook match")
	}
}
