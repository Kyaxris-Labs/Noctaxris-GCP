package cloudrun_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudrun"
)

func TestKnativeServiceListEnvelope(t *testing.T) {
	mux := mountCloudRun(t, nil)
	project := "noctaxris-gcp-local"
	createV2Service(t, mux, project, "demo", `{"template":{"containers":[{"image":"gcr.io/demo"}],"serviceAccount":"sa@example.com"}}`)

	req := httptest.NewRequest(http.MethodGet, "/apis/serving.knative.dev/v1/namespaces/"+project+"/services", nil)
	req.Host = "us-central1-run.googleapis.com"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["apiVersion"] != "serving.knative.dev/v1" || envelope["kind"] != "ServiceList" {
		t.Fatalf("envelope=%#v", envelope)
	}
	items, _ := envelope["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items=%#v", items)
	}
}

func TestKnativeServiceGetServiceAccountAndImage(t *testing.T) {
	mux := mountCloudRun(t, nil)
	project := "noctaxris-gcp-local"
	createV2Service(t, mux, project, "web", `{"template":{"containers":[{"image":"us-docker.pkg.dev/lib/app:1"}],"serviceAccount":"runtime@noctaxris-gcp-local.iam.gserviceaccount.com"}}`)

	req := httptest.NewRequest(http.MethodGet, "/apis/serving.knative.dev/v1/namespaces/"+project+"/services/web", nil)
	req.Host = cloudrun.DefaultLocation + "-127.0.0.1"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var svc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &svc); err != nil {
		t.Fatal(err)
	}
	if svc["kind"] != "Service" {
		t.Fatalf("kind=%v", svc["kind"])
	}
	spec, _ := svc["spec"].(map[string]any)
	template, _ := spec["template"].(map[string]any)
	pod, _ := template["spec"].(map[string]any)
	if pod["serviceAccountName"] != "runtime@noctaxris-gcp-local.iam.gserviceaccount.com" {
		t.Fatalf("serviceAccountName=%v", pod["serviceAccountName"])
	}
	containers, _ := pod["containers"].([]any)
	if len(containers) == 0 {
		t.Fatal("missing containers")
	}
	c0, _ := containers[0].(map[string]any)
	if c0["image"] != "us-docker.pkg.dev/lib/app:1" {
		t.Fatalf("image=%v", c0["image"])
	}
	status, _ := svc["status"].(map[string]any)
	conds, _ := status["conditions"].([]any)
	if len(conds) == 0 {
		t.Fatal("missing conditions")
	}
	ready, _ := conds[0].(map[string]any)
	if ready["status"] != "True" {
		t.Fatalf("condition status=%v (want True/False, not CONDITION_*)", ready["status"])
	}
}

func TestKnativeRevisionLabelSelector(t *testing.T) {
	mux := mountCloudRun(t, nil)
	project := "noctaxris-gcp-local"
	createV2Service(t, mux, project, "alpha", `{"template":{"containers":[{"image":"a"}]}}`)
	createV2Service(t, mux, project, "beta", `{"template":{"containers":[{"image":"b"}]}}`)

	path := "/apis/serving.knative.dev/v1/namespaces/" + project + "/revisions?labelSelector=serving.knative.dev/service=alpha"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "us-central1-run.googleapis.com"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("revisions status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["kind"] != "RevisionList" {
		t.Fatalf("kind=%v", envelope["kind"])
	}
	items, _ := envelope["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("want 1 revision for alpha, got %#v", items)
	}
	item, _ := items[0].(map[string]any)
	md, _ := item["metadata"].(map[string]any)
	labels, _ := md["labels"].(map[string]any)
	if labels["serving.knative.dev/service"] != "alpha" {
		t.Fatalf("labels=%#v", labels)
	}
	name, _ := md["name"].(string)
	if name != "alpha-00001" {
		t.Fatalf("short revision name=%q", name)
	}
}

func TestKnativeAuthzDeny(t *testing.T) {
	mux := mountCloudRun(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	req := httptest.NewRequest(http.MethodGet, "/apis/serving.knative.dev/v1/namespaces/noctaxris-gcp-local/services", nil)
	req.Host = "us-central1-run.googleapis.com"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestKnativeHostRegion(t *testing.T) {
	mux := mountCloudRun(t, nil)
	project := "noctaxris-gcp-local"
	// Create in default location via v2.
	createV2Service(t, mux, project, "regdemo", `{"template":{"containers":[{"image":"x"}]}}`)

	// Wrong region Host should list empty.
	req := httptest.NewRequest(http.MethodGet, "/apis/serving.knative.dev/v1/namespaces/"+project+"/services", nil)
	req.Host = "europe-west1-127.0.0.1"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d", rec.Code)
	}
	var empty map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &empty)
	items, _ := empty["items"].([]any)
	if len(items) != 0 {
		t.Fatalf("wrong region should be empty, got %#v", items)
	}

	// Matching region Host finds the service.
	req = httptest.NewRequest(http.MethodGet, "/apis/serving.knative.dev/v1/namespaces/"+project+"/services", nil)
	req.Host = cloudrun.DefaultLocation + "-127.0.0.1:4588"
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &empty)
	items, _ = empty["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("default region Host should find service, got %#v", items)
	}
}

func TestKnativeV2ListStillWorks(t *testing.T) {
	mux := mountCloudRun(t, nil)
	project := "noctaxris-gcp-local"
	createV2Service(t, mux, project, "v2still", `{"template":{"containers":[{"image":"z"}]}}`)

	path := "/v2/projects/" + project + "/locations/" + cloudrun.DefaultLocation + "/services"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("v2 list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	services, _ := body["services"].([]any)
	if len(services) != 1 {
		t.Fatalf("v2 services=%#v", services)
	}
}

func createV2Service(t *testing.T, mux *http.ServeMux, project, serviceID, body string) {
	t.Helper()
	base := "/v2/projects/" + project + "/locations/" + cloudrun.DefaultLocation + "/services"
	req := httptest.NewRequest(http.MethodPost, base+"?serviceId="+serviceID, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create %s status=%d body=%s", serviceID, rec.Code, rec.Body.String())
	}
}
