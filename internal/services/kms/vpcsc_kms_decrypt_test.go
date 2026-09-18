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

func TestVPCSCKmsDecryptOnly(t *testing.T) {
	t.Setenv("NOCTAXRIS_GCP_VPCSC_ENFORCE", "1")
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
	project := "noctaxris-gcp-local"
	rootSA := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, rootSA); err != nil {
		t.Fatal(err)
	}
	if err := st.MigrateAccessContextManager(); err != nil {
		t.Fatal(err)
	}
	status, _ := json.Marshal(map[string]any{
		"resources":          []string{"projects/" + project},
		"restrictedServices": []string{"cloudkms.googleapis.com"},
	})
	polName := store.AccessPolicyResourceName("kms-pol")
	if ok, err := st.CreateAccessPolicy(store.AccessPolicy{
		Name: polName, PolicyID: "kms-pol", Parent: "organizations/noctaxris-gcp-org", Title: "KMS",
	}); err != nil || !ok {
		t.Fatalf("policy ok=%v err=%v", ok, err)
	}
	if ok, err := st.CreateServicePerimeter(store.ServicePerimeter{
		Name: store.ServicePerimeterResourceName("kms-pol", "kms-edge"), PolicyName: polName,
		PerimeterID: "kms-edge", Title: "kms", StatusJSON: string(status),
	}); err != nil || !ok {
		t.Fatalf("perimeter ok=%v err=%v", ok, err)
	}

	var who authn.Principal
	who = authn.Principal{Email: rootSA, IsRoot: true}
	mux := http.NewServeMux()
	svc := &kms.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) { return who, true })

	loc := kms.DefaultLocation
	ringURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings?keyRingId=vpc-ring"
	req := httptest.NewRequest(http.MethodPost, ringURL, bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ring status=%d body=%s", rec.Code, rec.Body.String())
	}
	keyURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/vpc-ring/cryptoKeys?cryptoKeyId=vpc-key"
	req = httptest.NewRequest(http.MethodPost, keyURL, bytes.NewReader([]byte(`{"purpose":"ENCRYPT_DECRYPT"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("key status=%d body=%s", rec.Code, rec.Body.String())
	}

	plain := base64.StdEncoding.EncodeToString([]byte("secret-plain"))
	encURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/vpc-ring/cryptoKeys/vpc-key:encrypt"
	req = httptest.NewRequest(http.MethodPost, encURL, bytes.NewReader([]byte(`{"plaintext":"`+plain+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("encrypt status=%d body=%s", rec.Code, rec.Body.String())
	}
	var enc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &enc); err != nil {
		t.Fatal(err)
	}
	ct, _ := enc["ciphertext"].(string)

	who = authn.Principal{Email: "sa@other-proj.iam.gserviceaccount.com", IsRoot: true}
	decURL := "/v1/projects/" + project + "/locations/" + loc + "/keyRings/vpc-ring/cryptoKeys/vpc-key:decrypt"
	req = httptest.NewRequest(http.MethodPost, decURL, bytes.NewReader([]byte(`{"ciphertext":"`+ct+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-perimeter decrypt status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, encURL, bytes.NewReader([]byte(`{"plaintext":"`+plain+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("encrypt must not be perimeter-restricted status=%d body=%s", rec.Code, rec.Body.String())
	}
}
