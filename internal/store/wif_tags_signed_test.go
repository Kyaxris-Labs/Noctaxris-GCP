package store_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestWIFPoolAndProviderCRUD(t *testing.T) {
	st := openTestStore(t)
	p, err := st.CreateWIFPool("noctaxris-gcp-local", "global", "lab-pool", "Lab", "desc", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.State != "ACTIVE" || !strings.Contains(p.Name, "/workloadIdentityPools/lab-pool") {
		t.Fatalf("pool = %#v", p)
	}
	got, ok, err := st.GetWIFPool(p.Name)
	if err != nil || !ok || got.PoolID != "lab-pool" {
		t.Fatalf("get pool ok=%v err=%v %#v", ok, err, got)
	}
	prov, err := st.CreateWIFProvider(p.Name, "oidc-1", "OIDC", "", "https://example.com", `{"google.subject":"assertion.sub"}`, "[]", false)
	if err != nil {
		t.Fatal(err)
	}
	list, err := st.ListWIFProviders(p.Name, false)
	if err != nil || len(list) != 1 || list[0].ProviderID != "oidc-1" {
		t.Fatalf("providers = %#v err=%v", list, err)
	}
	if _, ok, err := st.DeleteWIFProvider(prov.Name); err != nil || !ok {
		t.Fatalf("delete provider ok=%v err=%v", ok, err)
	}
	if _, ok, err := st.DeleteWIFPool(p.Name); err != nil || !ok {
		t.Fatalf("delete pool ok=%v err=%v", ok, err)
	}
}

func TestCRMTagKeysAndBindings(t *testing.T) {
	st := openTestStore(t)
	k, err := st.CreateTagKey(store.DefaultOrganizationName, "env", "environment")
	if err != nil {
		t.Fatal(err)
	}
	if k.NamespacedName != "noctaxris-gcp-org/env" {
		t.Fatalf("namespaced = %q", k.NamespacedName)
	}
	b, err := st.CreateTagBinding("projects/noctaxris-gcp-local", "noctaxris-gcp-org/env/prod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(b.TagValue, "tagValues/") {
		t.Fatalf("tagValue = %q", b.TagValue)
	}
	list, err := st.ListTagBindings("projects/noctaxris-gcp-local")
	if err != nil || len(list) != 1 {
		t.Fatalf("bindings = %#v err=%v", list, err)
	}
}

func TestSecretRotationAndRotateTheatre(t *testing.T) {
	st := openTestStore(t)
	name := "projects/noctaxris-gcp-local/secrets/rot-sec"
	next := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339Nano)
	sec, created, err := st.CreateSecretWithRotation(name, "noctaxris-gcp-local", nil, nil, nil, "", "86400s", next, []map[string]string{{"name": "projects/p/topics/t"}})
	if err != nil || !created {
		t.Fatalf("create err=%v created=%v", err, created)
	}
	if sec.RotationPeriod != "86400s" || sec.NextRotationTime != next {
		t.Fatalf("rotation fields = %#v", sec)
	}
	if _, err := st.AddSecretVersion(name, []byte("v1")); err != nil {
		t.Fatal(err)
	}
	v, updated, err := st.RotateSecretTheatre(name, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.VersionID != "2" {
		t.Fatalf("version = %q", v.VersionID)
	}
	plain, _, err := st.AccessSecretVersion(name, "latest")
	if err != nil || string(plain) != "v1" {
		t.Fatalf("access = %q err=%v", plain, err)
	}
	if updated.NextRotationTime == next {
		t.Fatal("expected nextRotationTime to advance")
	}
}

func TestGCSV4SignedURLRoundTrip(t *testing.T) {
	host := "127.0.0.1:4588"
	path := "/storage/v1/b/lab/o/hello.txt"
	q := url.Values{"alt": []string{"media"}}
	signed, err := store.GenerateV4SignedURL(store.SignedURLRequest{
		Method:  "GET",
		Host:    host,
		Path:    path,
		Expires: 300,
		Query:   q,
		Now:     time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyV4SignedRequest(http.MethodGet, host, path, u.Query(), time.Date(2026, 7, 28, 12, 1, 0, 0, time.UTC)); err != nil {
		t.Fatalf("verify: %v url=%s", err, signed)
	}
	if err := store.VerifyV4SignedRequest(http.MethodGet, host, path, u.Query(), time.Date(2026, 7, 28, 13, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("expected expiry failure")
	}
}
