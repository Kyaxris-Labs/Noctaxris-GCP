package store_test

import (
	"encoding/json"
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
	var cfg map[string]any
	if err := json.Unmarshal([]byte(adv.BuildJSON), &cfg); err != nil {
		t.Fatal(err)
	}
	steps, _ := cfg["steps"].([]any)
	if len(steps) != 1 {
		t.Fatalf("steps=%#v", steps)
	}
	step0, _ := steps[0].(map[string]any)
	if step0["status"] != "SUCCESS" {
		t.Fatalf("step status=%#v", step0)
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

func TestArtifactBuildWorkflowsOpsStore(t *testing.T) {
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

	repoName := "projects/p/locations/us-central1/repositories/deep"
	ok, err := st.CreateArRepository(store.ArRepository{
		Name: repoName, ProjectID: "p", Location: "us-central1", RepositoryID: "deep", Format: "DOCKER",
	})
	if err != nil || !ok {
		t.Fatalf("create repo: %v %v", ok, err)
	}
	labels := `{"a":"1"}`
	repo, found, err := st.PatchArRepositoryDeepen(repoName, nil, &labels)
	if err != nil || !found || repo.LabelsJSON != labels {
		t.Fatalf("patch labels: %#v found=%v err=%v", repo, found, err)
	}
	pkgName := repoName + "/packages/img"
	_, _ = st.CreateArPackage(store.ArPackage{Name: pkgName, RepositoryName: repoName, PackageID: "img"})
	_, _ = st.CreateArVersion(store.ArVersion{
		Name: pkgName + "/versions/v1", PackageName: pkgName, VersionID: "v1",
		RelatedTagsJSON: `["latest"]`,
	})
	files, err := st.ListArFilesTheatreDeepen(repoName)
	if err != nil || len(files) != 1 {
		t.Fatalf("files: %v err=%v", files, err)
	}
	tags, err := st.ListArTagsTheatreDeepen(pkgName)
	if err != nil || len(tags) != 1 || tags[0].Name != pkgName+"/tags/latest" {
		t.Fatalf("tags: %#v err=%v", tags, err)
	}

	buildID := store.NewCbBuildID()
	bName := "projects/p/builds/" + buildID
	_, _ = st.CreateCbBuild(store.CbBuild{
		Name: bName, ProjectID: "p", Location: "global", BuildID: buildID, Status: "WORKING", BuildJSON: `{}`,
	})
	cancelled, found, err := st.CancelCbBuildDeepen(bName)
	if err != nil || !found || cancelled.Status != "CANCELLED" {
		t.Fatalf("cancel: %#v found=%v err=%v", cancelled, found, err)
	}

	wfName := "projects/p/locations/us-central1/workflows/deep"
	_, _ = st.CreateWorkflow(store.Workflow{
		Name: wfName, ProjectID: "p", Location: "us-central1", WorkflowID: "deep", SourceContents: "v1",
	})
	src := "v2"
	wf, found, err := st.PatchWorkflowDeepen(wfName, nil, &src, nil, nil)
	if err != nil || !found || wf.SourceContents != "v2" || wf.RevisionID == "000001-lab" {
		t.Fatalf("patch wf: %#v found=%v err=%v", wf, found, err)
	}
	exName := wfName + "/executions/e1"
	_, _ = st.CreateWorkflowExecution(store.WorkflowExecution{
		Name: exName, WorkflowName: wfName, ProjectID: "p", Location: "us-central1", WorkflowID: "deep",
		ExecutionID: "e1", State: "ACTIVE",
	})
	ex, found, err := st.CancelWorkflowExecutionDeepen(exName)
	if err != nil || !found || ex.State != "CANCELLED" {
		t.Fatalf("cancel ex: %#v found=%v err=%v", ex, found, err)
	}
	_, _ = st.CreateWorkflowExecution(store.WorkflowExecution{
		Name: wfName + "/executions/e2", WorkflowName: wfName, ProjectID: "p", Location: "us-central1", WorkflowID: "deep",
		ExecutionID: "e2", State: "ACTIVE",
	})
	page, next, err := st.ListWorkflowExecutionsPageDeepen(wfName, 1, "")
	if err != nil || len(page) != 1 || next == "" {
		t.Fatalf("page: %#v next=%q err=%v", page, next, err)
	}
}
