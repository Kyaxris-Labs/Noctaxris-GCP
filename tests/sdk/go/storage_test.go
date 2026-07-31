package sdk_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestListBucketsSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	status, body := doJSON(t, http.MethodGet, ep+"/storage/v1/b?project="+url.QueryEscape(project), token, nil)
	if status != http.StatusOK {
		t.Fatalf("list buckets status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode buckets: %v body=%s", err, body)
	}
	if kind, _ := parsed["kind"].(string); kind != "storage#buckets" {
		t.Fatalf("kind=%v want storage#buckets body=%s", parsed["kind"], body)
	}
}

func TestGCSGenerateSignedURLSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()
	bucket := uniqueID("sdk-signed")
	bucketPath := ep + "/storage/v1/b/" + url.PathEscape(bucket)

	status, body := doJSON(t, http.MethodPost, ep+"/storage/v1/b?project="+url.QueryEscape(project), token, map[string]any{
		"name": bucket,
	})
	if status != http.StatusOK {
		t.Fatalf("create bucket status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, bucketPath, token, nil)
	})

	status, body = doJSON(t, http.MethodPost, bucketPath+"/o/smoke.txt:generateSignedUrl", token, map[string]any{
		"method":  "GET",
		"expires": 600,
		"alt":     "media",
	})
	if status != http.StatusOK {
		t.Fatalf("generateSignedUrl status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode generateSignedUrl: %v body=%s", err, body)
	}
	if got, _ := parsed["signedUrl"].(string); got == "" {
		t.Fatalf("missing signedUrl body=%s", body)
	}
}

func TestGCSRetentionDeleteDenySmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()
	bucket := uniqueID("sdk-retain")
	bucketPath := ep + "/storage/v1/b/" + url.PathEscape(bucket)
	objectPath := bucketPath + "/o/" + url.PathEscape("held.txt")

	status, body := doJSON(t, http.MethodPost, ep+"/storage/v1/b?project="+url.QueryEscape(project), token, map[string]any{
		"name": bucket,
	})
	if status != http.StatusOK {
		t.Fatalf("create bucket status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, objectPath, token, nil)
		_, _, _ = doJSONErr(http.MethodDelete, bucketPath, token, nil)
	})

	status, body = doJSON(t, http.MethodPatch, bucketPath, token, map[string]any{
		"retentionPolicy": map[string]any{"retentionPeriod": "3600"},
	})
	if status != http.StatusOK {
		t.Fatalf("patch retention status=%d body=%s", status, body)
	}

	upReq, err := http.NewRequest(http.MethodPost, ep+"/upload/storage/v1/b/"+url.PathEscape(bucket)+"/o?uploadType=media&name="+url.QueryEscape("held.txt"), strings.NewReader("held"))
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	upReq.Header.Set("Authorization", "Bearer "+token)
	upReq.Header.Set("Content-Type", "text/plain")
	upResp, err := (&http.Client{Timeout: 5 * time.Second}).Do(upReq)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	upBody, _ := io.ReadAll(upResp.Body)
	_ = upResp.Body.Close()
	if upResp.StatusCode != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", upResp.StatusCode, upBody)
	}

	status, body = doJSON(t, http.MethodDelete, objectPath, token, nil)
	if status == http.StatusOK {
		t.Fatalf("delete under retention should fail; got status=%d body=%s", status, body)
	}
	var errBody map[string]any
	if err := json.Unmarshal(body, &errBody); err != nil {
		t.Fatalf("decode delete error: %v body=%s", err, body)
	}
	e, _ := errBody["error"].(map[string]any)
	if e == nil || e["status"] != "FAILED_PRECONDITION" {
		t.Fatalf("want FAILED_PRECONDITION, got status=%d body=%s", status, body)
	}
}

func TestListBigtableInstancesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v2/projects/" + project + "/instances"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list bigtable instances status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode bigtable instances: %v body=%s", err, body)
	}
	if _, ok := parsed["instances"]; !ok {
		t.Fatalf("missing instances field body=%s", body)
	}
}

func TestListMemorystoreInstancesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/locations/us-central1/instances"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list memorystore instances status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode memorystore instances: %v body=%s", err, body)
	}
	if _, ok := parsed["instances"]; !ok {
		t.Fatalf("missing instances field body=%s", body)
	}
}

func TestListManagedKafkaClustersSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/locations/us-central1/clusters"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list managed kafka clusters status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode managed kafka clusters: %v body=%s", err, body)
	}
	if _, ok := parsed["clusters"]; !ok {
		t.Fatalf("missing clusters field body=%s", body)
	}
}

func TestListCloudSQLInstancesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/sql/v1/projects/" + project + "/instances"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list cloudsql instances status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode cloudsql instances: %v body=%s", err, body)
	}
	if _, ok := parsed["items"]; !ok {
		t.Fatalf("missing items field body=%s", body)
	}
}

func TestListBigQueryDatasetsSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/bigquery/v2/projects/" + project + "/datasets"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list BigQuery datasets status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode BigQuery datasets: %v body=%s", err, body)
	}
	if _, ok := parsed["datasets"]; !ok {
		t.Fatalf("missing datasets field body=%s", body)
	}
}

func TestListSpannerInstancesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/instances"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list Spanner instances status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode Spanner instances: %v body=%s", err, body)
	}
	if _, ok := parsed["instances"]; !ok {
		t.Fatalf("missing instances field body=%s", body)
	}
}
