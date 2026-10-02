package iam_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
)

func TestSignBlobSignJwtEvaluateAnySAResource(t *testing.T) {
	h := openIAM(t)
	const project = "noctaxris-gcp-local"
	target := seedServiceAccount(t, h.store, project, "sign-target")
	caller := seedServiceAccount(t, h.store, project, "sign-caller")
	saRes := "projects/" + project + "/serviceAccounts/" + target

	if err := h.store.PutIAMPolicyJSON(saRes, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/iam.serviceAccountTokenCreator",
			Members: []string{"serviceAccount:" + caller},
		}},
		Etag: "ACAB",
	}); err != nil {
		t.Fatal(err)
	}

	h.setWho(caller, false)
	base := "/v1/projects/" + project + "/serviceAccounts"
	do := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.mux.ServeHTTP(rec, req)
		return rec
	}

	blob := base64.StdEncoding.EncodeToString([]byte("sa-scoped-sign"))
	if rec := do(base+"/"+target+":signBlob", `{"bytesToSign":"`+blob+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("SA-scoped TokenCreator signBlob status=%d body=%s", rec.Code, rec.Body.String())
	}
	payload := fmt.Sprintf(`{"payload":"{\"iss\":\"lab\",\"sub\":\"lab\",\"aud\":\"x\",\"exp\":%d}"}`, time.Now().Add(time.Hour).Unix())
	if rec := do(base+"/"+target+":signJwt", payload); rec.Code != http.StatusOK {
		t.Fatalf("SA-scoped TokenCreator signJwt status=%d body=%s", rec.Code, rec.Body.String())
	}

	stranger := seedServiceAccount(t, h.store, project, "sign-stranger")
	h.setWho(stranger, false)
	if rec := do(base+"/"+target+":signBlob", `{"bytesToSign":"`+blob+`"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("unrelated caller signBlob want 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
