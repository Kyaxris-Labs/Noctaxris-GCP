package orgpolicy_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestOrgPolicyGetEffectiveAndUnknownConstraint(t *testing.T) {
	h := open(t)
	constraint := store.ConstraintDisableServiceAccountKeyCreation
	parent := "projects/noctaxris-gcp-local"
	body := `{"name":"` + parent + `/policies/` + constraint + `","spec":{"rules":[{"enforce":true}]}}`
	req := httptest.NewRequest(http.MethodPost, "/v2/"+parent+"/policies?constraint="+constraint, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v2/"+parent+"/policies/"+constraint+":getEffectivePolicy", nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("effective: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v2/"+parent+"/policies?constraint=not.a.real.constraint",
		bytes.NewReader([]byte(`{"spec":{"rules":[{"enforce":true}]}}`)))
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("unknown constraint must fail")
	}
}
