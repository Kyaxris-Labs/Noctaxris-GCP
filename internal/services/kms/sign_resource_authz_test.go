package kms_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/kms"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestSignAndPublicKeyResourceLevelAuthz(t *testing.T) {
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
	const project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	signer := "signer@" + project + ".iam.gserviceaccount.com"
	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID: project, Email: signer, UniqueID: "signer", DisplayName: "signer",
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	who := authn.Principal{Email: root, IsRoot: true}
	(&kms.Service{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}).Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return who, true
	})
	loc := kms.DefaultLocation
	base := "/v1/projects/" + project + "/locations/" + loc
	req := httptest.NewRequest(http.MethodPost, base+"/keyRings?keyRingId=ring1", bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create ring status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, base+"/keyRings/ring1/cryptoKeys?cryptoKeyId=signkey",
		bytes.NewReader([]byte(`{"purpose":"ASYMMETRIC_SIGN","versionTemplate":{"algorithm":"RSA_SIGN_PSS_2048_SHA256"}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create key status=%d body=%s", rec.Code, rec.Body.String())
	}
	keyName := "projects/" + project + "/locations/" + loc + "/keyRings/ring1/cryptoKeys/signkey"
	if err := st.PutIAMPolicyJSON(keyName, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/cloudkms.signerVerifier",
			Members: []string{"serviceAccount:" + signer},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	who = authn.Principal{Email: signer, IsRoot: false}
	digest := base64.StdEncoding.EncodeToString(make([]byte, 32))
	signBody, _ := json.Marshal(map[string]any{"digest": map[string]string{"sha256": digest}})
	req = httptest.NewRequest(http.MethodPost, base+"/keyRings/ring1/cryptoKeys/signkey/cryptoKeyVersions/1:asymmetricSign", bytes.NewReader(signBody))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resource-level sign status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/keyRings/ring1/cryptoKeys/signkey/cryptoKeyVersions/1/publicKey", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resource-level publicKey status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Key without binding must deny.
	req = httptest.NewRequest(http.MethodPost, base+"/keyRings?keyRingId=ring2", bytes.NewReader([]byte("{}")))
	who = authn.Principal{Email: root, IsRoot: true}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	req = httptest.NewRequest(http.MethodPost, base+"/keyRings/ring2/cryptoKeys?cryptoKeyId=other",
		bytes.NewReader([]byte(`{"purpose":"ASYMMETRIC_SIGN","versionTemplate":{"algorithm":"RSA_SIGN_PSS_2048_SHA256"}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	who = authn.Principal{Email: signer, IsRoot: false}
	req = httptest.NewRequest(http.MethodPost, base+"/keyRings/ring2/cryptoKeys/other/cryptoKeyVersions/1:asymmetricSign", bytes.NewReader(signBody))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unbound key sign status=%d body=%s", rec.Code, rec.Body.String())
	}
}
