package cloudasset_test

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
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudasset"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/resourcemanager"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func mountCloudAsset(t *testing.T) (*http.ServeMux, string) {
	t.Helper()
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
	const project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateBucket("lab-asset-bucket", project, "US", "STANDARD"); err != nil {
		t.Fatal(err)
	}
	topicName := "projects/" + project + "/topics/lab-asset-topic"
	if _, _, err := st.CreateTopic(topicName, project); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	principal := func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: root, IsRoot: true}, true
	}
	svc := &cloudasset.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, principal)
	crm := &resourcemanager.Handler{
		Store: st, Authz: &authz.Evaluator{Policies: st}, Principal: principal,
	}
	crm.Mount(mux)
	return mux, project
}

func TestSearchListExportFeed(t *testing.T) {
	mux, project := mountCloudAsset(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+":searchAllResources", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status=%d body=%s", rec.Code, rec.Body.String())
	}
	var searchResp struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &searchResp); err != nil {
		t.Fatal(err)
	}
	if len(searchResp.Results) < 3 {
		t.Fatalf("expected project+bucket+topic(+SA): %#v", searchResp.Results)
	}
	foundBucket := false
	for _, r := range searchResp.Results {
		if at, _ := r["assetType"].(string); at == "storage.googleapis.com/Bucket" {
			foundBucket = true
		}
	}
	if !foundBucket {
		t.Fatalf("bucket missing from search: %#v", searchResp.Results)
	}

	req = httptest.NewRequest(http.MethodGet,
		"/v1/projects/"+project+":searchAllResources?assetTypes=storage.googleapis.com/Bucket&query=name:lab-asset", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered search status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &searchResp)
	if len(searchResp.Results) != 1 {
		t.Fatalf("filtered search want 1 got %#v", searchResp.Results)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/assets", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("listAssets status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		Assets []map[string]any `json:"assets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Assets) < 3 {
		t.Fatalf("listAssets %#v", listResp.Assets)
	}

	exportBody := `{"outputConfig":{"gcsDestination":{"uri":"gs://lab-asset-bucket/export.json"}},"assetTypes":["storage.googleapis.com/Bucket"]}`
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+":exportAssets", bytes.NewReader([]byte(exportBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("exportAssets status=%d body=%s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	if op["done"] != true {
		t.Fatalf("export expected done op: %#v", op)
	}
	name, _ := op["name"].(string)
	if !strings.Contains(name, "/operations/") {
		t.Fatalf("export op name: %#v", op["name"])
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+":batchGetAssetsHistory", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("batchGetAssetsHistory status=%d body=%s", rec.Code, rec.Body.String())
	}
	var histResp struct {
		Assets []map[string]any `json:"assets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &histResp); err != nil {
		t.Fatal(err)
	}
	if len(histResp.Assets) < 1 {
		t.Fatalf("expected history after export: %#v", histResp)
	}

	feedBody := `{"assetTypes":["storage.googleapis.com/Bucket"],"contentType":"RESOURCE","feedOutputConfig":{"pubsubDestination":{"topic":"projects/` + project + `/topics/lab-asset-topic"}}}`
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/feeds?feedId=lab-feed", bytes.NewReader([]byte(feedBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("createFeed status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/feeds", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("listFeeds status=%d body=%s", rec.Code, rec.Body.String())
	}
	var feedsResp struct {
		Feeds []map[string]any `json:"feeds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &feedsResp); err != nil {
		t.Fatal(err)
	}
	if len(feedsResp.Feeds) != 1 {
		t.Fatalf("feeds %#v", feedsResp.Feeds)
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/projects/"+project+"/feeds/lab-feed", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("deleteFeed status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudAssetAuthzDeny(t *testing.T) {
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
	const project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	viewer := "viewer@" + project + ".iam.gserviceaccount.com"
	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID: project, Email: viewer, UniqueID: "viewer1", DisplayName: "viewer",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/viewer",
			Members: []string{"serviceAccount:" + viewer},
		}},
		Etag: "ACAB",
	}); err != nil {
		t.Fatal(err)
	}

	eval := &authz.Evaluator{Policies: st}
	mux := http.NewServeMux()
	svc := &cloudasset.Service{Store: st, Authz: eval}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: viewer, IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+":searchAllResources", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer search status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+":exportAssets",
		bytes.NewReader([]byte(`{"outputConfig":{"gcsDestination":{"uri":"gs://x/y"}}}`)))
	rec = httptest.NewRecorder()
	cloudasset.HandleProjectExport(rec, req, st, eval, project, authn.Principal{Email: viewer, IsRoot: false})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer export want 403 got %d body=%s", rec.Code, rec.Body.String())
	}
}
