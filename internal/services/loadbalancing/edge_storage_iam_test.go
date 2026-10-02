package loadbalancing_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/loadbalancing"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestLBEdgeDeniesWithoutStorageIAM(t *testing.T) {
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
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateBucket("priv-bucket", project, "US", "STANDARD"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutObjectBytes("priv-bucket", "o.txt", "text/plain", []byte("secret")); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	lb := &loadbalancing.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	lb.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		if strings.HasPrefix(r.URL.Path, "/lb/") {
			return authn.Principal{}, false
		}
		return authn.Principal{Email: root, IsRoot: true}, true
	})

	bsBody := `{"name":"priv-bs","backends":[{"gcsBucket":"priv-bucket"}]}`
	req := httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/backendServices", bytes.NewReader([]byte(bsBody)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("backend: %d %s", rec.Code, rec.Body.String())
	}
	selfLink := "projects/" + project + "/global/backendServices/priv-bs"
	mapPayload, _ := json.Marshal(map[string]any{"name": "priv-map", "defaultService": selfLink})
	req = httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/urlMaps", bytes.NewReader(mapPayload))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("url map: %d", rec.Code)
	}
	frPayload, _ := json.Marshal(map[string]any{"name": "priv-fr", "target": "projects/" + project + "/global/urlMaps/priv-map"})
	req = httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/forwardingRules", bytes.NewReader(frPayload))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("fr: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/lb/"+project+"/priv-fr/o.txt", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("anonymous without allUsers want 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	if err := st.PutIAMPolicyJSON(store.BucketIAMResource("priv-bucket"), authz.Policy{
		Bindings: []authz.Binding{{Role: "roles/storage.objectViewer", Members: []string{"allUsers"}}},
	}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/lb/"+project+"/priv-fr/o.txt", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "secret" {
		t.Fatalf("allUsers edge: %d %q", rec.Code, rec.Body.String())
	}
}
