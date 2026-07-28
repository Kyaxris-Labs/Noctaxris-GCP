package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestArtifactRegistryStoreCRUD(t *testing.T) {
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

	repoName := "projects/p/locations/us-central1/repositories/docker-lab"
	ok, err := st.CreateArRepository(store.ArRepository{
		Name: repoName, ProjectID: "p", Location: "us-central1", RepositoryID: "docker-lab",
		Format: "DOCKER", Description: "lab",
	})
	if err != nil || !ok {
		t.Fatalf("create repo: ok=%v err=%v", ok, err)
	}
	repo, found, err := st.GetArRepository(repoName)
	if err != nil || !found || repo.Format != "DOCKER" {
		t.Fatalf("get repo: %#v found=%v err=%v", repo, found, err)
	}
	list, err := st.ListArRepositories("p", "us-central1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list repos: %v err=%v", list, err)
	}

	pkgName := repoName + "/packages/hello"
	ok, err = st.CreateArPackage(store.ArPackage{
		Name: pkgName, RepositoryName: repoName, PackageID: "hello", DisplayName: "hello",
	})
	if err != nil || !ok {
		t.Fatalf("create package: ok=%v err=%v", ok, err)
	}
	verName := pkgName + "/versions/sha256:abc"
	ok, err = st.CreateArVersion(store.ArVersion{
		Name: verName, PackageName: pkgName, VersionID: "sha256:abc", Description: "v1",
	})
	if err != nil || !ok {
		t.Fatalf("create version: ok=%v err=%v", ok, err)
	}
	vers, err := st.ListArVersions(pkgName)
	if err != nil || len(vers) != 1 {
		t.Fatalf("list versions: %v err=%v", vers, err)
	}
	ok, err = st.DeleteArVersion(verName)
	if err != nil || !ok {
		t.Fatalf("delete version: ok=%v err=%v", ok, err)
	}
	ok, err = st.DeleteArPackage(pkgName)
	if err != nil || !ok {
		t.Fatalf("delete package: ok=%v err=%v", ok, err)
	}
	ok, err = st.DeleteArRepository(repoName)
	if err != nil || !ok {
		t.Fatalf("delete repo: ok=%v err=%v", ok, err)
	}
}

func TestCloudBuildStoreTheatre(t *testing.T) {
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

	buildID := store.NewCbBuildID()
	name := "projects/p/builds/" + buildID
	ok, err := st.CreateCbBuild(store.CbBuild{
		Name: name, ProjectID: "p", Location: "global", BuildID: buildID,
		Status: "WORKING", BuildJSON: `{"steps":[{"name":"gcr.io/cloud-builders/docker"}]}`,
	})
	if err != nil || !ok {
		t.Fatalf("create build: ok=%v err=%v", ok, err)
	}
	adv, found, err := st.AdvanceCbBuildToSuccess(name)
	if err != nil || !found || adv.Status != "SUCCESS" {
		t.Fatalf("advance: %#v found=%v err=%v", adv, found, err)
	}
	if adv.FinishTime == "" {
		t.Fatal("expected finishTime")
	}

	trigName := "projects/p/locations/global/triggers/t1"
	ok, err = st.CreateCbTrigger(store.CbTrigger{
		Name: trigName, ProjectID: "p", Location: "global", TriggerID: "t1",
		TriggerJSON: `{"filename":"cloudbuild.yaml"}`,
	})
	if err != nil || !ok {
		t.Fatalf("create trigger: ok=%v err=%v", ok, err)
	}
	trigs, err := st.ListCbTriggers("p", "global")
	if err != nil || len(trigs) != 1 {
		t.Fatalf("list triggers: %v err=%v", trigs, err)
	}
	ok, err = st.DeleteCbTrigger(trigName)
	if err != nil || !ok {
		t.Fatalf("delete trigger: ok=%v err=%v", ok, err)
	}
}
