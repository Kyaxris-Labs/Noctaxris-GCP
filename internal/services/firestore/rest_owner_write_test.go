package firestore_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	fsvc "github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/firestore"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestFirestoreRESTOwnerWriteAllowAndDeny(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	project := "noctaxris-gcp-local"
	if err := st.EnsureRoot(project, "root@"+project+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}

	var who authn.Principal
	mux := http.NewServeMux()
	svc := &fsvc.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.MountREST(mux, func(*http.Request) (authn.Principal, bool) { return who, true })

	body := `{"fields":{"role":{"stringValue":"admin"}}}`
	who = authn.Principal{Email: "uid-own", IsRoot: false}
	req := httptest.NewRequest(http.MethodPost,
		"/v1/projects/"+project+"/databases/(default)/documents/users?documentId=uid-own",
		strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["name"] != "projects/"+project+"/databases/(default)/documents/users/uid-own" {
		t.Fatalf("name %#v", doc["name"])
	}

	req = httptest.NewRequest(http.MethodPatch,
		"/v1/projects/"+project+"/databases/(default)/documents/users/uid-own",
		strings.NewReader(`{"fields":{"role":{"stringValue":"user"}}}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner patch status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost,
		"/v1/projects/"+project+"/databases/(default)/documents/users?documentId=uid-other",
		strings.NewReader(body))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("other uid create status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch,
		"/v1/projects/"+project+"/databases/(default)/documents/users/uid-other",
		strings.NewReader(body))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("other uid patch status=%d body=%s", rec.Code, rec.Body.String())
	}
}
