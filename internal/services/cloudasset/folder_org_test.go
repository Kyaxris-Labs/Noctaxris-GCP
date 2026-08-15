package cloudasset_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCloudAssetFolderOrgAndGetFeed(t *testing.T) {
	mux, project := mountCloudAsset(t)
	org := "noctaxris-gcp-org"
	folder := "lab-folder"

	req := httptest.NewRequest(http.MethodGet, "/v1/organizations/"+org+":searchAllResources", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("org search: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/folders/"+folder+":searchAllResources", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("folder search: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/organizations/"+org+"/assets", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("org list assets: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/folders/"+folder+"/assets", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("folder list assets: %d %s", rec.Code, rec.Body.String())
	}

	exportBody := `{"outputConfig":{"gcsDestination":{"uri":"gs://lab-asset-bucket/org-export.json"}}}`
	req = httptest.NewRequest(http.MethodPost, "/v1/organizations/"+org+":exportAssets", bytes.NewReader([]byte(exportBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("org export: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/folders/"+folder+":exportAssets", bytes.NewReader([]byte(exportBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("folder export: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/organizations/"+org+":batchGetAssetsHistory", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("org history: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/folders/"+folder+":batchGetAssetsHistory", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("folder history: %d %s", rec.Code, rec.Body.String())
	}

	feedBody := `{"assetTypes":["storage.googleapis.com/Bucket"],"contentType":"RESOURCE","feedOutputConfig":{"pubsubDestination":{"topic":"projects/` + project + `/topics/lab-asset-topic"}}}`
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/feeds?feedId=get-me", bytes.NewReader([]byte(feedBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create feed: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet,
		"/v1/projects/"+project+":searchAllResources?assetTypes=storage.googleapis.com/Bucket,pubsub.googleapis.com/Topic", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("csv assetTypes search: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/feeds/get-me", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get feed: %d %s", rec.Code, rec.Body.String())
	}
	var feed map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &feed)
	if feed["name"] == nil {
		t.Fatalf("feed=%#v", feed)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/feeds/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing feed: %d", rec.Code)
	}
}
