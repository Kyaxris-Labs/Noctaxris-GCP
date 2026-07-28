package vertexai_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/vertexai"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestVertexAIAllowlistedPredictAndGenerateContent(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	svc := &vertexai.Service{Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	base := "/v1/projects/noctaxris-gcp-local/locations/" + vertexai.DefaultLocation + "/publishers/google/models"

	req := httptest.NewRequest(http.MethodPost, base+"/gemini-1.5-flash:generateContent",
		bytes.NewReader([]byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("generateContent status=%d body=%s", rec.Code, rec.Body.String())
	}
	var gen map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &gen)
	cands, _ := gen["candidates"].([]any)
	if len(cands) != 1 {
		t.Fatalf("generateContent=%#v", gen)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/text-bison:predict",
		bytes.NewReader([]byte(`{"instances":[{"content":"hi"}]}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("predict status=%d body=%s", rec.Code, rec.Body.String())
	}
	var pred map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &pred)
	preds, _ := pred["predictions"].([]any)
	if len(preds) != 1 {
		t.Fatalf("predict=%#v", pred)
	}
}

func TestVertexAIUnknownModelFailClosed(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	svc := &vertexai.Service{Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	req := httptest.NewRequest(http.MethodPost,
		"/v1/projects/noctaxris-gcp-local/locations/us-central1/publishers/google/models/not-a-real-model:predict",
		bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestVertexAIAuthzFailClosed(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	svc := &vertexai.Service{Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodPost,
		"/v1/projects/noctaxris-gcp-local/locations/us-central1/publishers/google/models/gemini-1.5-flash:generateContent",
		bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
