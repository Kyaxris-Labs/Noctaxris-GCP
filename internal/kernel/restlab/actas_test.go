package restlab_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

type saRows map[string]store.ServiceAccount

func (m saRows) GetServiceAccount(email string) (store.ServiceAccount, bool, error) {
	sa, ok := m[email]
	return sa, ok, nil
}

type errSALookup struct{ err error }

func (e errSALookup) GetServiceAccount(string) (store.ServiceAccount, bool, error) {
	return store.ServiceAccount{}, false, e.err
}

func TestRequireServiceAccountActAsPositiveNegative(t *testing.T) {
	email := "runner@noctaxris-gcp-local.iam.gserviceaccount.com"
	caller := "caller@noctaxris-gcp-local.iam.gserviceaccount.com"
	const home = "p"
	saRes := "projects/" + home + "/serviceAccounts/" + email
	rows := saRows{email: {ProjectID: home, Email: email}}
	ev := &authz.Evaluator{Policies: memPolicies{
		saRes: mustPolicy(t, "roles/iam.serviceAccountUser", "serviceAccount:"+caller),
	}}

	rec := httptest.NewRecorder()
	if !restlab.RequireServiceAccountActAs(rec, ev, rows, authn.Principal{Email: caller}, email) {
		t.Fatalf("actAs denied: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	if restlab.RequireServiceAccountActAs(rec, ev, rows, authn.Principal{Email: "other@noctaxris-gcp-local.iam.gserviceaccount.com"}, email) {
		t.Fatal("unbound caller must deny")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	if !restlab.RequireServiceAccountActAs(rec, ev, rows, authn.Principal{IsRoot: true, Email: "root"}, email) {
		t.Fatal("root must allow")
	}

	rec = httptest.NewRecorder()
	if restlab.RequireServiceAccountActAs(rec, nil, rows, authn.Principal{Email: caller}, email) {
		t.Fatal("nil evaluator must deny non-root")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	if restlab.RequireServiceAccountActAs(rec, ev, rows, authn.Principal{Email: caller}, "") {
		t.Fatal("empty email must deny")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("empty status=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	if restlab.RequireServiceAccountActAs(rec, ev, rows, authn.Principal{Email: caller}, "   ") {
		t.Fatal("blank email must deny")
	}

	rec = httptest.NewRecorder()
	if restlab.RequireServiceAccountActAs(rec, ev, rows, authn.Principal{Email: caller}, "missing@noctaxris-gcp-local.iam.gserviceaccount.com") {
		t.Fatal("unknown email must deny")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unknown status=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	if restlab.RequireServiceAccountActAs(rec, ev, nil, authn.Principal{Email: caller}, email) {
		t.Fatal("nil lookup must deny")
	}

	rec = httptest.NewRecorder()
	if restlab.RequireServiceAccountActAs(rec, ev, errSALookup{err: errors.New("lookup")}, authn.Principal{Email: caller}, email) {
		t.Fatal("lookup error must stop")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("lookup status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRequireServiceAccountActAsUsesHomeProject(t *testing.T) {
	const projectP = "p"
	const projectQ = "q"
	const defaultProject = "noctaxris-gcp-local"
	owner := "owner@p.iam.gserviceaccount.com"
	homeSA := "runner@p.iam.gserviceaccount.com"
	foreignSA := "foreign@q.iam.gserviceaccount.com"
	rootSA := "root@noctaxris-gcp-local.iam.gserviceaccount.com"
	prefixOwner := "owner@proj.iam.gserviceaccount.com"
	prefixSA := "runner@proj-extra.iam.gserviceaccount.com"

	rows := saRows{
		homeSA:    {ProjectID: projectP, Email: homeSA},
		foreignSA: {ProjectID: projectQ, Email: foreignSA},
		rootSA:    {ProjectID: defaultProject, Email: rootSA},
		prefixSA:  {ProjectID: "proj-extra", Email: prefixSA},
	}
	// Owner of P is granted on P and on the SA that lives in P.
	// The same principal has no binding on Q, the default project, or proj-extra.
	// A prefix project policy is present so a string-prefix match would allow.
	ev := &authz.Evaluator{Policies: memPolicies{
		"projects/" + projectP:                                mustPolicy(t, "roles/owner", "serviceAccount:"+owner),
		"projects/" + projectP + "/serviceAccounts/" + homeSA: mustPolicy(t, "roles/iam.serviceAccountUser", "serviceAccount:"+owner),
		"projects/proj":                                       mustPolicy(t, "roles/owner", "serviceAccount:"+prefixOwner),
		"projects/proj/serviceAccounts/" + prefixSA:           mustPolicy(t, "roles/iam.serviceAccountUser", "serviceAccount:"+prefixOwner),
	}}

	rec := httptest.NewRecorder()
	if !restlab.RequireServiceAccountActAs(rec, ev, rows, authn.Principal{Email: owner}, homeSA) {
		t.Fatalf("owner of home project denied: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	if restlab.RequireServiceAccountActAs(rec, ev, rows, authn.Principal{Email: owner}, foreignSA) {
		t.Fatal("owner of P must not actAs an SA in Q")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	if restlab.RequireServiceAccountActAs(rec, ev, rows, authn.Principal{Email: owner}, rootSA) {
		t.Fatal("owner of P must not actAs the root SA in the default project")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("root SA status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	if restlab.RequireServiceAccountActAs(rec, ev, rows, authn.Principal{Email: prefixOwner}, prefixSA) {
		t.Fatal("caller project that is only a prefix of the SA project must deny")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("prefix status=%d body=%s", rec.Code, rec.Body.String())
	}
}
