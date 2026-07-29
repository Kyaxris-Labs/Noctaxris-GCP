package store_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func openACMStore(t *testing.T) *store.Store {
	t.Helper()
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
	if err := st.MigrateAccessContextManager(); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestAccessPolicyAndPerimeterCRUD(t *testing.T) {
	st := openACMStore(t)
	polName := store.AccessPolicyResourceName("lab-policy")
	ok, err := st.CreateAccessPolicy(store.AccessPolicy{
		Name: polName, PolicyID: "lab-policy", Parent: "organizations/noctaxris-gcp-org", Title: "Lab",
	})
	if err != nil || !ok {
		t.Fatalf("create policy: ok=%v err=%v", ok, err)
	}
	ok, err = st.CreateAccessPolicy(store.AccessPolicy{
		Name: polName, PolicyID: "lab-policy", Parent: "organizations/noctaxris-gcp-org",
	})
	if err != nil || ok {
		t.Fatalf("duplicate policy expected ok=false: ok=%v err=%v", ok, err)
	}
	got, found, err := st.GetAccessPolicy(polName)
	if err != nil || !found || got.Title != "Lab" {
		t.Fatalf("get policy: %#v found=%v err=%v", got, found, err)
	}
	list, err := st.ListAccessPolicies("organizations/noctaxris-gcp-org")
	if err != nil || len(list) != 1 {
		t.Fatalf("list policies: n=%d err=%v", len(list), err)
	}

	status, _ := json.Marshal(map[string]any{
		"resources":          []string{"projects/proj-a"},
		"restrictedServices": []string{"storage.googleapis.com", "pubsub.googleapis.com"},
	})
	spName := store.ServicePerimeterResourceName("lab-policy", "perimeter-a")
	ok, err = st.CreateServicePerimeter(store.ServicePerimeter{
		Name: spName, PolicyName: polName, PerimeterID: "perimeter-a",
		Title: "A", StatusJSON: string(status),
	})
	if err != nil || !ok {
		t.Fatalf("create perimeter: ok=%v err=%v", ok, err)
	}
	sp, found, err := st.GetServicePerimeter(spName)
	if err != nil || !found || sp.Title != "A" {
		t.Fatalf("get perimeter: %#v found=%v err=%v", sp, found, err)
	}
	perims, err := st.ListServicePerimeters(polName)
	if err != nil || len(perims) != 1 {
		t.Fatalf("list perimeters: n=%d err=%v", len(perims), err)
	}
	deleted, err := st.DeleteServicePerimeter(spName)
	if err != nil || !deleted {
		t.Fatalf("delete perimeter: deleted=%v err=%v", deleted, err)
	}
	okDel, err := st.DeleteAccessPolicy(polName)
	if err != nil || !okDel {
		t.Fatalf("delete policy: ok=%v err=%v", okDel, err)
	}
}

func TestVPCSCDenyCrossPerimeterEnforce(t *testing.T) {
	st := openACMStore(t)
	t.Setenv("NOCTAXRIS_GCP_VPCSC_ENFORCE", "")
	if err := st.VPCSCDenyCrossPerimeter("proj-a", "proj-b", "pubsub.googleapis.com"); err != nil {
		t.Fatalf("enforce off must allow: %v", err)
	}

	polName := store.AccessPolicyResourceName("vpcsc-pol")
	if _, err := st.CreateAccessPolicy(store.AccessPolicy{
		Name: polName, PolicyID: "vpcsc-pol", Parent: "organizations/noctaxris-gcp-org", Title: "VPCSC",
	}); err != nil {
		t.Fatal(err)
	}
	status, _ := json.Marshal(map[string]any{
		"resources":          []string{"projects/proj-a"},
		"restrictedServices": []string{"pubsub.googleapis.com", "storage.googleapis.com"},
	})
	if _, err := st.CreateServicePerimeter(store.ServicePerimeter{
		Name:       store.ServicePerimeterResourceName("vpcsc-pol", "p1"),
		PolicyName: polName, PerimeterID: "p1", StatusJSON: string(status),
	}); err != nil {
		t.Fatal(err)
	}

	t.Setenv("NOCTAXRIS_GCP_VPCSC_ENFORCE", "1")
	if !store.VPCSCEnforceEnabled() {
		t.Fatal("expected enforce enabled")
	}
	if err := st.VPCSCDenyCrossPerimeter("proj-a", "proj-a", "pubsub.googleapis.com"); err != nil {
		t.Fatalf("same project must allow: %v", err)
	}
	err := st.VPCSCDenyCrossPerimeter("proj-a", "proj-b", "pubsub.googleapis.com")
	if !errors.Is(err, store.ErrVPCSCPerimeter) {
		t.Fatalf("cross-perimeter expected ErrVPCSCPerimeter, got %v", err)
	}
	if err := st.VPCSCDenyCrossPerimeter("proj-a", "proj-b", "bigquery.googleapis.com"); err != nil {
		t.Fatalf("unrestricted service must allow: %v", err)
	}
}

func TestVPCSCDryRunEnforceOptional(t *testing.T) {
	st := openACMStore(t)
	polName := store.AccessPolicyResourceName("dry-pol")
	if _, err := st.CreateAccessPolicy(store.AccessPolicy{
		Name: polName, PolicyID: "dry-pol", Parent: "organizations/noctaxris-gcp-org",
	}); err != nil {
		t.Fatal(err)
	}
	spec, _ := json.Marshal(map[string]any{
		"resources":          []string{"projects/inside"},
		"restrictedServices": []string{"storage.googleapis.com"},
	})
	if _, err := st.CreateServicePerimeter(store.ServicePerimeter{
		Name:       store.ServicePerimeterResourceName("dry-pol", "dry1"),
		PolicyName: polName, PerimeterID: "dry1",
		SpecJSON: string(spec), UseExplicitDryRunSpec: true,
	}); err != nil {
		t.Fatal(err)
	}

	t.Setenv("NOCTAXRIS_GCP_VPCSC_ENFORCE", "1")
	err := st.VPCSCDenyCrossPerimeter("inside", "outside", "storage.googleapis.com")
	if !errors.Is(err, store.ErrVPCSCPerimeter) {
		t.Fatalf("dry-run with enforce on must deny: %v", err)
	}
}

func TestProjectIDFromServiceAccountEmail(t *testing.T) {
	got := store.ProjectIDFromServiceAccountEmail("sa@my-lab.iam.gserviceaccount.com")
	if got != "my-lab" {
		t.Fatalf("got %q", got)
	}
	if store.ProjectIDFromServiceAccountEmail("user@example.com") != "" {
		t.Fatal("expected empty for non-SA")
	}
}
