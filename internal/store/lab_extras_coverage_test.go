package store_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestHMACKeyCRUDAndLabSecret(t *testing.T) {
	st := openTestStore(t)
	const project = "noctaxris-gcp-local"
	sa := "app@" + project + ".iam.gserviceaccount.com"

	_, err := st.CreateHMACKey("", sa)
	if err == nil {
		t.Fatal("expected empty project error")
	}
	_, err = st.CreateHMACKey(project, "")
	if err == nil {
		t.Fatal("expected empty sa error")
	}

	k, err := st.CreateHMACKey(project, sa)
	if err != nil {
		t.Fatal(err)
	}
	if k.AccessID == "" || k.Secret == "" || k.State != "ACTIVE" {
		t.Fatalf("key=%#v", k)
	}

	got, ok, err := st.GetHMACKey(k.AccessID)
	if err != nil || !ok || got.Secret != k.Secret {
		t.Fatalf("get ok=%v err=%v got=%#v", ok, err, got)
	}
	lab, ok, err := st.GetHMACKey(store.LabGCSHMACAccessID)
	if err != nil || !ok || lab.Secret != store.LabGCSHMACSecret {
		t.Fatalf("lab key ok=%v err=%v got=%#v", ok, err, lab)
	}
	_, ok, err = st.GetHMACKey("missing-access-id")
	if err != nil || ok {
		t.Fatalf("missing get ok=%v err=%v", ok, err)
	}

	list, err := st.ListHMACKeys(project)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%#v err=%v", list, err)
	}
	sec, ok, err := st.HMACSecret(k.AccessID)
	if err != nil || !ok || sec != k.Secret {
		t.Fatalf("secret ok=%v err=%v sec=%q", ok, err, sec)
	}

	deleted, err := st.DeleteHMACKey(k.AccessID)
	if err != nil || !deleted {
		t.Fatalf("delete deleted=%v err=%v", deleted, err)
	}
	deleted, err = st.DeleteHMACKey(k.AccessID)
	if err != nil || deleted {
		t.Fatalf("second delete deleted=%v err=%v", deleted, err)
	}
	_, ok, err = st.HMACSecret(k.AccessID)
	if err != nil || ok {
		t.Fatalf("secret after delete ok=%v err=%v", ok, err)
	}
}

func TestIdentityTenantCRUD(t *testing.T) {
	st := openTestStore(t)
	const project = "noctaxris-gcp-local"

	_, _, err := st.CreateIdentityTenant(store.IdentityTenant{ProjectID: "", TenantID: "t1"})
	if err == nil {
		t.Fatal("expected validation error")
	}
	ten, created, err := st.CreateIdentityTenant(store.IdentityTenant{
		ProjectID: project, TenantID: "tenant-a", DisplayName: "A", AllowPasswordSignup: true,
	})
	if err != nil || !created {
		t.Fatalf("create created=%v err=%v", created, err)
	}
	if ten.Name != "projects/"+project+"/tenants/tenant-a" {
		t.Fatalf("name=%q", ten.Name)
	}
	_, created, err = st.CreateIdentityTenant(store.IdentityTenant{
		ProjectID: project, TenantID: "tenant-a", DisplayName: "dup",
	})
	if err != nil || created {
		t.Fatalf("duplicate created=%v err=%v", created, err)
	}

	got, ok, err := st.GetIdentityTenant(project, "tenant-a")
	if err != nil || !ok || !got.AllowPasswordSignup {
		t.Fatalf("get ok=%v err=%v got=%#v", ok, err, got)
	}
	_, ok, err = st.GetIdentityTenant(project, "missing")
	if err != nil || ok {
		t.Fatalf("missing ok=%v err=%v", ok, err)
	}
	list, err := st.ListIdentityTenants(project)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%#v err=%v", list, err)
	}

	allow := false
	name := "Renamed"
	patched, ok, err := st.PatchIdentityTenant(project, "tenant-a", &allow, &name)
	if err != nil || !ok || patched.AllowPasswordSignup || patched.DisplayName != "Renamed" {
		t.Fatalf("patch ok=%v err=%v got=%#v", ok, err, patched)
	}
	_, ok, err = st.PatchIdentityTenant(project, "missing", &allow, nil)
	if err != nil || ok {
		t.Fatalf("patch missing ok=%v err=%v", ok, err)
	}
}

func TestBinaryAuthzPolicyAndAdmission(t *testing.T) {
	st := openTestStore(t)
	const project = "noctaxris-gcp-local"
	image := "us-docker.pkg.dev/lib/app@sha256:deadbeef"

	allow, err := st.BinaryAuthzAllows(project, image)
	if err != nil || !allow {
		t.Fatalf("default allow=%v err=%v", allow, err)
	}

	if err := st.PutBinaryAuthzPolicy(project, "ENFORCED_BLOCK_AND_AUDIT_LOG", `{"note":"lab"}`); err != nil {
		t.Fatal(err)
	}
	mode, body, ok, err := st.GetBinaryAuthzPolicy(project)
	if err != nil || !ok || mode != "ENFORCED_BLOCK_AND_AUDIT_LOG" || body == "" {
		t.Fatalf("get mode=%q body=%q ok=%v err=%v", mode, body, ok, err)
	}
	allow, err = st.BinaryAuthzAllows(project, image)
	if err != nil || allow {
		t.Fatalf("enforced without attestation allow=%v err=%v", allow, err)
	}
	allow, err = st.BinaryAuthzAllows(project, "")
	if err != nil || allow {
		t.Fatalf("empty image allow=%v err=%v", allow, err)
	}

	if err := st.PutContainerOccurrence(store.ContainerOccurrence{
		Name:         "projects/" + project + "/occurrences/att-1",
		ProjectID:    project,
		OccurrenceID: "att-1",
		ResourceURI:  image,
		Kind:         "ATTESTATION",
		NoteName:     "projects/" + project + "/notes/lab",
	}); err != nil {
		t.Fatal(err)
	}
	occ, ok, err := st.GetContainerOccurrence("projects/" + project + "/occurrences/att-1")
	if err != nil || !ok || occ.ResourceURI != image {
		t.Fatalf("get occ ok=%v err=%v got=%#v", ok, err, occ)
	}
	list, err := st.ListContainerOccurrences(project, image)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%#v err=%v", list, err)
	}
	allow, err = st.BinaryAuthzAllows(project, image)
	if err != nil || !allow {
		t.Fatalf("with attestation allow=%v err=%v", allow, err)
	}

	if err := st.PutBinaryAuthzPolicy(project, "DRYRUN_AUDIT_LOG_ONLY", `{}`); err != nil {
		t.Fatal(err)
	}
	allow, err = st.BinaryAuthzAllows(project, "unattested:latest")
	if err != nil || !allow {
		t.Fatalf("dryrun allow=%v err=%v", allow, err)
	}
}

func TestCbWorkerPoolCRUD(t *testing.T) {
	st := openTestStore(t)
	const project = "noctaxris-gcp-local"
	_, _, err := st.CreateCbWorkerPool(store.CbWorkerPool{ProjectID: project, Location: "", PoolID: "p1"})
	if err == nil {
		t.Fatal("expected validation error")
	}
	p, created, err := st.CreateCbWorkerPool(store.CbWorkerPool{
		ProjectID: project, Location: "us-central1", PoolID: "pool-a",
		ConfigJSON: `{"workerConfig":{"machineType":"e2-medium"}}`,
	})
	if err != nil || !created {
		t.Fatalf("create created=%v err=%v", created, err)
	}
	if p.Name != "projects/"+project+"/locations/us-central1/workerPools/pool-a" {
		t.Fatalf("name=%q", p.Name)
	}
	_, created, err = st.CreateCbWorkerPool(store.CbWorkerPool{
		ProjectID: project, Location: "us-central1", PoolID: "pool-a",
	})
	if err != nil || created {
		t.Fatalf("dup created=%v err=%v", created, err)
	}
	got, ok, err := st.GetCbWorkerPool(p.Name)
	if err != nil || !ok || got.PoolID != "pool-a" {
		t.Fatalf("get ok=%v err=%v got=%#v", ok, err, got)
	}
	list, err := st.ListCbWorkerPools(project, "us-central1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%#v err=%v", list, err)
	}
	all, err := st.ListCbWorkerPools(project, "")
	if err != nil || len(all) != 1 {
		t.Fatalf("list all=%#v err=%v", all, err)
	}
}

func TestCreateProjectPositiveNegative(t *testing.T) {
	st := openTestStore(t)
	_, created, err := st.CreateProject(store.Project{ID: ""})
	if err == nil || created {
		t.Fatalf("empty id created=%v err=%v", created, err)
	}
	_, created, err = st.CreateProject(store.Project{ID: "bad/id"})
	if err == nil || created {
		t.Fatalf("slash id created=%v err=%v", created, err)
	}
	p, created, err := st.CreateProject(store.Project{ID: "lab-extra-proj", DisplayName: "Lab Extra"})
	if err != nil || !created {
		t.Fatalf("create created=%v err=%v", created, err)
	}
	if p.State != "ACTIVE" || p.DisplayName != "Lab Extra" {
		t.Fatalf("project=%#v", p)
	}
	_, created, err = st.CreateProject(store.Project{ID: "lab-extra-proj"})
	if err != nil || created {
		t.Fatalf("duplicate created=%v err=%v", created, err)
	}
}
