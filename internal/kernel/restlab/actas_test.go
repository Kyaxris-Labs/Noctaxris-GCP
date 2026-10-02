package restlab_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
)

func TestRequireServiceAccountActAsPositiveNegative(t *testing.T) {
	email := "runner@noctaxris-gcp-local.iam.gserviceaccount.com"
	caller := "caller@noctaxris-gcp-local.iam.gserviceaccount.com"
	saRes := "projects/p/serviceAccounts/" + email
	ev := &authz.Evaluator{Policies: memPolicies{
		saRes: mustPolicy(t, "roles/iam.serviceAccountUser", "serviceAccount:"+caller),
	}}

	rec := httptest.NewRecorder()
	if !restlab.RequireServiceAccountActAs(rec, ev, authn.Principal{Email: caller}, "p", email) {
		t.Fatalf("actAs denied: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	if restlab.RequireServiceAccountActAs(rec, ev, authn.Principal{Email: "other@noctaxris-gcp-local.iam.gserviceaccount.com"}, "p", email) {
		t.Fatal("unbound caller must deny")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	if !restlab.RequireServiceAccountActAs(rec, ev, authn.Principal{IsRoot: true, Email: "root"}, "p", email) {
		t.Fatal("root must allow")
	}

	rec = httptest.NewRecorder()
	if restlab.RequireServiceAccountActAs(rec, nil, authn.Principal{Email: caller}, "p", email) {
		t.Fatal("nil evaluator must deny non-root")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	if !restlab.RequireServiceAccountActAs(rec, ev, authn.Principal{Email: caller}, "p", "") {
		t.Fatal("empty email must skip actAs")
	}
}
