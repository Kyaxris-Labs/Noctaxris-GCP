package iam_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
)

func TestIamCredentialsPathAliasGenerateAccessToken(t *testing.T) {
	h := openIAM(t)
	const project = "noctaxris-gcp-local"
	h.setWho("root@"+project+".iam.gserviceaccount.com", true)
	email := seedServiceAccount(t, h.store, project, "impersonatee")

	req := httptest.NewRequest(http.MethodPost,
		"/iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/"+email+":generateAccessToken",
		bytes.NewReader([]byte(`{"scope":["https://www.googleapis.com/auth/cloud-platform"]}`)))
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("alias status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["accessToken"] == nil || out["expireTime"] == nil {
		t.Fatalf("token shape %#v", out)
	}
}

func TestTokenCreatorRequestTimeCEL(t *testing.T) {
	h := openIAM(t)
	const project = "noctaxris-gcp-local"
	caller := seedServiceAccount(t, h.store, project, "caller")
	target := seedServiceAccount(t, h.store, project, "target")
	if err := h.store.PutIAMPolicyJSON("projects/"+project+"/serviceAccounts/"+target, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/iam.serviceAccountTokenCreator",
			Members: []string{"serviceAccount:" + caller},
			Condition: &authz.Expr{
				Expression: `request.time < timestamp("2021-01-01T00:00:00Z")`,
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	eval := &authz.Evaluator{Policies: h.store, Roles: h.store}
	fixed := time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC)
	eval.Now = func() time.Time { return fixed }
	ok, err := eval.Evaluate(caller, false, "iam.serviceAccounts.getAccessToken", "projects/"+project+"/serviceAccounts/"+target)
	if err != nil || !ok {
		t.Fatalf("in-window: ok=%v err=%v", ok, err)
	}
	eval.Now = func() time.Time { return time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC) }
	ok, err = eval.Evaluate(caller, false, "iam.serviceAccounts.getAccessToken", "projects/"+project+"/serviceAccounts/"+target)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expired request.time condition must deny")
	}
}
