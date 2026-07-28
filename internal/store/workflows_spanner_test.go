package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestWorkflowsAndSpannerStoreCRUD(t *testing.T) {
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

	wfName := "projects/p/locations/us-central1/workflows/wf1"
	created, err := st.CreateWorkflow(store.Workflow{
		Name: wfName, ProjectID: "p", Location: "us-central1", WorkflowID: "wf1",
		SourceContents: "main:\n  steps:\n    - done:\n        return: ok\n",
	})
	if err != nil || !created {
		t.Fatalf("create workflow: created=%v err=%v", created, err)
	}
	wf, ok, err := st.GetWorkflow(wfName)
	if err != nil || !ok || wf.SourceContents == "" {
		t.Fatalf("get workflow: ok=%v err=%v %#v", ok, err, wf)
	}
	list, err := st.ListWorkflows("p", "us-central1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list workflows: %#v err=%v", list, err)
	}

	exName := wfName + "/executions/e1"
	exOK, err := st.CreateWorkflowExecution(store.WorkflowExecution{
		Name: exName, WorkflowName: wfName, ProjectID: "p", Location: "us-central1", WorkflowID: "wf1",
		ExecutionID: "e1", Argument: `{"x":1}`, Result: `{"ok":true}`, State: "SUCCEEDED",
	})
	if err != nil || !exOK {
		t.Fatalf("create execution: %v %v", exOK, err)
	}
	exs, err := st.ListWorkflowExecutions(wfName)
	if err != nil || len(exs) != 1 {
		t.Fatalf("list executions: %#v err=%v", exs, err)
	}

	instName := "projects/p/instances/lab"
	instOK, err := st.CreateSpannerInstance(store.SpannerInstance{
		Name: instName, ProjectID: "p", InstanceID: "lab",
		Config: "projects/p/instanceConfigs/regional-us-central1", DisplayName: "Lab",
	})
	if err != nil || !instOK {
		t.Fatalf("create instance: %v %v", instOK, err)
	}
	dbName := instName + "/databases/app"
	dbOK, err := st.CreateSpannerDatabase(store.SpannerDatabase{
		Name: dbName, InstanceName: instName, ProjectID: "p", InstanceID: "lab", DatabaseID: "app",
		CreateStatement: "CREATE DATABASE `app`",
	})
	if err != nil || !dbOK {
		t.Fatalf("create database: %v %v", dbOK, err)
	}
	sess, sessOK, err := st.CreateSpannerSession(store.SpannerSession{
		DatabaseName: dbName, ProjectID: "p", InstanceID: "lab", DatabaseID: "app",
	})
	if err != nil || !sessOK || sess.Name == "" {
		t.Fatalf("create session: %#v ok=%v err=%v", sess, sessOK, err)
	}

	if ok, err := st.DeleteWorkflow(wfName); err != nil || !ok {
		t.Fatalf("delete workflow: %v %v", ok, err)
	}
	if _, ok, _ := st.GetWorkflowExecution(exName); ok {
		t.Fatal("expected executions cascaded on workflow delete")
	}
	if ok, err := st.DeleteSpannerInstance(instName); err != nil || !ok {
		t.Fatalf("delete instance: %v %v", ok, err)
	}
	if _, ok, _ := st.GetSpannerDatabase(dbName); ok {
		t.Fatal("expected databases cascaded on instance delete")
	}
}
