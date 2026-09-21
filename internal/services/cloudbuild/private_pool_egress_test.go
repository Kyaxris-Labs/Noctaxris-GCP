package cloudbuild_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudbuild"
)

func TestPrivatePoolNoPublicEgressDeniesWANAllowsLabGCS(t *testing.T) {
	st, mux, svc := setupStepExec(t)
	poolReq := httptest.NewRequest(http.MethodPost,
		"/v1/projects/noctaxris-gcp-local/locations/us-central1/workerPools?workerPoolId=pool-a",
		bytes.NewReader([]byte(`{"displayName":"private"}`)))
	poolRec := httptest.NewRecorder()
	mux.ServeHTTP(poolRec, poolReq)
	if poolRec.Code != http.StatusOK {
		t.Fatalf("create pool status=%d body=%s", poolRec.Code, poolRec.Body.String())
	}
	poolName := "projects/noctaxris-gcp-local/locations/us-central1/workerPools/pool-a"

	var ran atomic.Int32
	svc.StepRunner = &cloudbuild.EngineRunner{
		Store: st,
		ExecuteStep: func(context.Context, cloudbuild.BuildStep) error {
			ran.Add(1)
			return nil
		},
	}

	wan := `{"steps":[{"name":"alpine:3.23","args":["curl","https://example.com"]}],"options":{"pool":{"name":"` + poolName + `"}}}`
	created := postBuild(t, mux, wan)
	id, _ := created["id"].(string)
	got := getBuild(t, mux, id)
	if got["status"] != "FAILURE" {
		t.Fatalf("WAN URL must fail: %#v", got)
	}
	detail, _ := got["statusDetail"].(string)
	if !strings.Contains(detail, "NO_PUBLIC_EGRESS") {
		t.Fatalf("want NO_PUBLIC_EGRESS detail, got %q", detail)
	}
	if ran.Load() != 0 {
		t.Fatal("ExecuteStep must not run when egress is denied")
	}

	gcs := `{"steps":[{"name":"alpine:3.23","args":["gsutil","cp","out","http://127.0.0.1:4588/storage/v1/b/scratch/o/out"]}],"options":{"pool":{"name":"` + poolName + `"}}}`
	created = postBuild(t, mux, gcs)
	id, _ = created["id"].(string)
	got = getBuild(t, mux, id)
	if got["status"] != "SUCCESS" {
		t.Fatalf("in-emulator GCS URL must be allowed: %#v", got)
	}
	if ran.Load() != 1 {
		t.Fatalf("ExecuteStep ran %d times, want 1", ran.Load())
	}

	nested := `{"steps":[{"name":"alpine:3.23","args":["curl","http://host.docker.internal:4588/iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/x:generateAccessToken"]}],"options":{"pool":{"name":"` + poolName + `"}}}`
	created = postBuild(t, mux, nested)
	id, _ = created["id"].(string)
	got = getBuild(t, mux, id)
	if got["status"] != "SUCCESS" {
		t.Fatalf("host.docker.internal:4588 must be allowed: %#v", got)
	}
	if ran.Load() != 2 {
		t.Fatalf("ExecuteStep ran %d times, want 2", ran.Load())
	}

	badPort := `{"steps":[{"name":"alpine:3.23","args":["curl","http://host.docker.internal:9/x"]}],"options":{"pool":{"name":"` + poolName + `"}}}`
	created = postBuild(t, mux, badPort)
	id, _ = created["id"].(string)
	got = getBuild(t, mux, id)
	if got["status"] != "FAILURE" {
		t.Fatalf("host.docker.internal non-lab port must fail: %#v", got)
	}
}
