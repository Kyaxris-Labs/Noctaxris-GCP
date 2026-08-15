package loadbalancing_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/loadbalancing"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestURLMapAndForwardingRuleCRUD(t *testing.T) {
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
	project := "noctaxris-gcp-local"
	if err := st.EnsureRoot(project, "root@"+project+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	lb := &loadbalancing.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	lb.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@" + project + ".iam.gserviceaccount.com", IsRoot: true}, true
	})

	bsBody := `{"name":"bs1","backends":[{"gcsBucket":"b","objectPrefix":"p"}]}`
	req := httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/backendServices", bytes.NewReader([]byte(bsBody)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("backend: %d %s", rec.Code, rec.Body.String())
	}
	selfLink := "projects/" + project + "/global/backendServices/bs1"

	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/global/backendServices", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list backends: %d", rec.Code)
	}

	mapPayload, _ := json.Marshal(map[string]any{"name": "um1", "defaultService": selfLink})
	req = httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/urlMaps", bytes.NewReader(mapPayload))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("urlmap create: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/global/urlMaps", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list urlmaps: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/global/urlMaps/um1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get urlmap: %d %s", rec.Code, rec.Body.String())
	}
	var um map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &um)
	mapLink, _ := um["selfLink"].(string)
	if mapLink == "" {
		mapLink = "projects/" + project + "/global/urlMaps/um1"
	}

	frPayload, _ := json.Marshal(map[string]any{"name": "fr1", "target": mapLink})
	req = httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/forwardingRules", bytes.NewReader(frPayload))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("fr create: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/global/forwardingRules", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list fr: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/global/forwardingRules/fr1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get fr: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodHead, "/lb/"+project+"/fr1/", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	// invoke may 404 without bucket object; still covers handler entry
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
		t.Fatalf("head lb: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, "/compute/v1/projects/"+project+"/global/forwardingRules/fr1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete fr: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, "/compute/v1/projects/"+project+"/global/urlMaps/um1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete urlmap: %d %s", rec.Code, rec.Body.String())
	}
}
