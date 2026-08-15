package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCRMTagsGetListDelete(t *testing.T) {
	st := openIdentityStore(t)
	k, err := st.CreateTagKey(store.DefaultOrganizationName, "tier", "tier tag")
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.GetTagKey(k.Name)
	if err != nil || !ok || got.ShortName != "tier" {
		t.Fatalf("get key: %#v ok=%v err=%v", got, ok, err)
	}
	_, ok, err = st.GetTagKey("missing-key")
	if err != nil || ok {
		t.Fatalf("missing key ok=%v err=%v", ok, err)
	}
	list, err := st.ListTagKeys(store.DefaultOrganizationName)
	if err != nil || len(list) < 1 {
		t.Fatalf("list keys: n=%d err=%v", len(list), err)
	}
	b, err := st.CreateTagBinding("projects/noctaxris-gcp-local", "noctaxris-gcp-org/tier/gold")
	if err != nil {
		t.Fatal(err)
	}
	gb, ok, err := st.GetTagBinding(b.Name)
	if err != nil || !ok || gb.TagValueNamespacedName == "" {
		t.Fatalf("get binding: %#v ok=%v err=%v", gb, ok, err)
	}
	delB, err := st.DeleteTagBinding(b.Name)
	if err != nil || !delB {
		t.Fatalf("delete binding: %v err=%v", delB, err)
	}
	delK, err := st.DeleteTagKey(k.Name)
	if err != nil || !delK {
		t.Fatalf("delete key: %v err=%v", delK, err)
	}
}

func TestEnsureServiceAccountAndDeleteKey(t *testing.T) {
	st := openIdentityStore(t)
	email := "ensured@noctaxris-gcp-local.iam.gserviceaccount.com"
	if err := st.EnsureServiceAccount("noctaxris-gcp-local", email, "Ensured"); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureServiceAccount("noctaxris-gcp-local", email, "Ignored"); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureServiceAccount("", email, ""); err == nil {
		t.Fatal("expected error for empty project")
	}
	plain := []byte(`{"type":"service_account","token":"ens-token"}`)
	sealed, err := st.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	keyName := "projects/noctaxris-gcp-local/serviceAccounts/" + email + "/keys/k1"
	now := time.Now().UTC().Format(time.RFC3339)
	if err := st.CreateServiceAccountKey(store.ServiceAccountKey{
		Name: keyName, SAEmail: email, PrivateKeyData: sealed,
		ValidAfterTime: now, ValidBeforeTime: "9999-12-31T23:59:59Z",
	}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.GetServiceAccountKey(keyName)
	if err != nil || !ok || got.SAEmail != email {
		t.Fatalf("get key: %#v ok=%v err=%v", got, ok, err)
	}
	_, ok, err = st.GetServiceAccountKey(keyName + "-missing")
	if err != nil || ok {
		t.Fatalf("missing key ok=%v err=%v", ok, err)
	}
	deleted, err := st.DeleteServiceAccountKey(keyName)
	if err != nil || !deleted {
		t.Fatalf("delete key: %v err=%v", deleted, err)
	}
	deleted, err = st.DeleteServiceAccountKey(keyName)
	if err != nil || deleted {
		t.Fatalf("second delete: %v err=%v", deleted, err)
	}
	_ = authn.HashToken("x")
}

func TestAccessPolicyAndPerimeterUpdate(t *testing.T) {
	st := openACMStore(t)
	polName := store.AccessPolicyResourceName("upd-policy")
	ok, err := st.CreateAccessPolicy(store.AccessPolicy{
		Name: polName, PolicyID: "upd-policy", Parent: "organizations/noctaxris-gcp-org", Title: "Before",
	})
	if err != nil || !ok {
		t.Fatalf("create: ok=%v err=%v", ok, err)
	}
	upd, found, err := st.UpdateAccessPolicy(polName, "After", `["projects/noctaxris-gcp-local"]`, true, true)
	if err != nil || !found || upd.Title != "After" {
		t.Fatalf("update policy: %#v found=%v err=%v", upd, found, err)
	}
	_, found, err = st.UpdateAccessPolicy("accessPolicies/missing", "x", "[]", true, false)
	if err != nil || found {
		t.Fatalf("missing policy update found=%v err=%v", found, err)
	}
	spName := store.ServicePerimeterResourceName("upd-policy", "p1")
	ok, err = st.CreateServicePerimeter(store.ServicePerimeter{
		Name: spName, PolicyName: polName, PerimeterID: "p1", Title: "P", StatusJSON: `{}`,
	})
	if err != nil || !ok {
		t.Fatalf("create perimeter: ok=%v err=%v", ok, err)
	}
	updated, found, err := st.UpdateServicePerimeter(store.ServicePerimeter{
		Name: spName, Title: "P2", Description: "d", PerimeterType: "PERIMETER_TYPE_REGULAR",
		StatusJSON: `{"resources":["projects/x"]}`, SpecJSON: `{}`, BodyJSON: `{"title":"P2"}`,
	})
	if err != nil || !found || updated.Title != "P2" {
		t.Fatalf("update perimeter: %#v found=%v err=%v", updated, found, err)
	}
	if !errors.Is(nil, nil) {
		t.Fatal("noop")
	}
}
