package kms_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/kms"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

type vpcscKMSFixture struct {
	mux     *http.ServeMux
	st      *store.Store
	who     authn.Principal
	project string
	encURL  string
	decURL  string
	ct      string
	plain   string
}

func setupVPCSCKms(t *testing.T, resources []string) *vpcscKMSFixture {
	t.Helper()
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
		"resources":          resources,
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

	f := &vpcscKMSFixture{
		st:      st,
		who:     authn.Principal{Email: rootSA, IsRoot: true},
		project: project,
	}
	mux := http.NewServeMux()
	svc := &kms.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) { return f.who, true })
	f.mux = mux

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

	f.plain = base64.StdEncoding.EncodeToString([]byte("secret-plain"))
	f.encURL = "/v1/projects/" + project + "/locations/" + loc + "/keyRings/vpc-ring/cryptoKeys/vpc-key:encrypt"
	req = httptest.NewRequest(http.MethodPost, f.encURL, bytes.NewReader([]byte(`{"plaintext":"`+f.plain+`"}`)))
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
	f.ct = ct
	f.decURL = "/v1/projects/" + project + "/locations/" + loc + "/keyRings/vpc-ring/cryptoKeys/vpc-key:decrypt"
	return f
}

func (f *vpcscKMSFixture) decrypt() *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, f.decURL, bytes.NewReader([]byte(`{"ciphertext":"`+f.ct+`"}`)))
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

func TestVPCSCKmsDecryptOnly(t *testing.T) {
	f := setupVPCSCKms(t, []string{"projects/noctaxris-gcp-local"})

	f.who = authn.Principal{Email: "sa@other-proj.iam.gserviceaccount.com", IsRoot: true}
	rec := f.decrypt()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-perimeter decrypt status=%d body=%s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, f.encURL, bytes.NewReader([]byte(`{"plaintext":"`+f.plain+`"}`)))
	rec = httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("encrypt must not be perimeter-restricted status=%d body=%s", rec.Code, rec.Body.String())
	}

	f.who = authn.Principal{Email: "root@" + f.project + ".iam.gserviceaccount.com", IsRoot: true}
	rec = f.decrypt()
	if rec.Code != http.StatusOK {
		t.Fatalf("same-project decrypt status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestVPCSCKmsDecryptWIFUnresolvedDenies(t *testing.T) {
	f := setupVPCSCKms(t, []string{"projects/noctaxris-gcp-local"})
	f.who = authn.Principal{Email: "wif:missing:alice", IsRoot: true}
	rec := f.decrypt()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unresolved WIF decrypt status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestVPCSCKmsDecryptWIFSameProjectAllows(t *testing.T) {
	f := setupVPCSCKms(t, []string{"projects/noctaxris-gcp-local"})
	pool, err := f.st.CreateWIFPool(f.project, "global", "kms-pool", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CreateWIFProvider(pool.Name, "oidc-lab", "", "", "", "{}", "[]", false); err != nil {
		t.Fatal(err)
	}
	f.who = authn.Principal{Email: "wif:oidc-lab:alice", IsRoot: true}
	rec := f.decrypt()
	if rec.Code != http.StatusOK {
		t.Fatalf("same-project WIF decrypt status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestVPCSCKmsDecryptWIFOtherProjectDenies(t *testing.T) {
	f := setupVPCSCKms(t, []string{"projects/noctaxris-gcp-local"})
	pool, err := f.st.CreateWIFPool("other-proj", "global", "kms-pool", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CreateWIFProvider(pool.Name, "oidc-lab", "", "", "", "{}", "[]", false); err != nil {
		t.Fatal(err)
	}
	f.who = authn.Principal{Email: "wif:oidc-lab:alice", IsRoot: true}
	rec := f.decrypt()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-project WIF decrypt status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestVPCSCKmsDecryptHostUserDenies(t *testing.T) {
	f := setupVPCSCKms(t, []string{"projects/noctaxris-gcp-local"})
	f.who = authn.Principal{Email: "player@example.com", IsRoot: true}
	rec := f.decrypt()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("host user decrypt status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Request is denied because of VPC Service Controls") {
		t.Fatalf("want VPC-SC message, got %s", rec.Body.String())
	}
}

func TestVPCSCKmsDecryptProjectNumberPerimeter(t *testing.T) {
	project := "noctaxris-gcp-local"
	f := setupVPCSCKms(t, []string{"projects/" + store.LabProjectNumber(project)})
	f.who = authn.Principal{Email: "sa@other-proj.iam.gserviceaccount.com", IsRoot: true}
	rec := f.decrypt()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("number-perimeter decrypt status=%d body=%s", rec.Code, rec.Body.String())
	}
}
