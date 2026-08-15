package store_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestStoreSpannerAndAppEngineCRM(t *testing.T) {
	st := openTestStore(t)
	project := "noctaxris-gcp-local"

	instName := "projects/" + project + "/instances/sp1"
	ok, err := st.CreateSpannerInstance(store.SpannerInstance{
		Name: instName, ProjectID: project, InstanceID: "sp1", Config: "regional-us-central1", DisplayName: "sp1",
	})
	if err != nil || !ok {
		t.Fatalf("create instance: %v %v", ok, err)
	}
	inst, found, err := st.GetSpannerInstance(instName)
	if err != nil || !found || inst.InstanceID != "sp1" {
		t.Fatal(err)
	}
	insts, err := st.ListSpannerInstances(project)
	if err != nil || len(insts) < 1 {
		t.Fatalf("list inst: %v", insts)
	}
	dbName := instName + "/databases/db1"
	ok, err = st.CreateSpannerDatabase(store.SpannerDatabase{
		Name: dbName, InstanceName: instName, DatabaseID: "db1", ProjectID: project, InstanceID: "sp1",
	})
	if err != nil || !ok {
		t.Fatalf("create db: %v %v", ok, err)
	}
	db, found, err := st.GetSpannerDatabase(dbName)
	if err != nil || !found {
		t.Fatal(err)
	}
	dbs, err := st.ListSpannerDatabases(instName)
	if err != nil || len(dbs) != 1 || db.DatabaseID == "" {
		t.Fatalf("list db: %v", dbs)
	}
	udb, found, err := st.AppendSpannerDDL(dbName, []string{"CREATE TABLE t (id INT64) PRIMARY KEY(id)"})
	if err != nil || !found || udb.DDLStatementsJSON == "" || udb.DDLStatementsJSON == "[]" {
		t.Fatalf("ddl %#v", udb)
	}
	sess, created, err := st.CreateSpannerSession(store.SpannerSession{
		DatabaseName: dbName, ProjectID: project, InstanceID: "sp1", DatabaseID: "db1",
	})
	if err != nil || !created {
		t.Fatalf("session: %v %v", created, err)
	}
	gs, found, err := st.GetSpannerSession(sess.Name)
	if err != nil || !found || gs.Name == "" {
		t.Fatal(err)
	}
	if err := st.InsertSpannerRows(dbName, "t", []string{"id"}, [][]string{{"1"}, {"2"}}); err != nil {
		t.Fatal(err)
	}
	rows, err := st.ListSpannerRows(dbName, "t")
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%v", rows)
	}
	ok, err = st.DeleteSpannerDatabase(dbName)
	if err != nil || !ok {
		t.Fatal(err)
	}
	ok, err = st.DeleteSpannerInstance(instName)
	if err != nil || !ok {
		t.Fatal(err)
	}

	wfName := "projects/" + project + "/locations/us-central1/workflows/w1"
	ok, err = st.CreateWorkflow(store.Workflow{
		Name: wfName, ProjectID: project, Location: "us-central1", WorkflowID: "w1", SourceContents: "main: return 1",
	})
	if err != nil || !ok {
		t.Fatalf("workflow: %v %v", ok, err)
	}
	wf, found, err := st.GetWorkflow(wfName)
	if err != nil || !found {
		t.Fatal(err)
	}
	wfs, err := st.ListWorkflows(project, "us-central1")
	if err != nil || len(wfs) < 1 || wf.WorkflowID == "" {
		t.Fatalf("list wf: %v", wfs)
	}
	exName := wfName + "/executions/e1"
	ok, err = st.CreateWorkflowExecution(store.WorkflowExecution{
		Name: exName, WorkflowName: wfName, ProjectID: project, Location: "us-central1",
		WorkflowID: "w1", ExecutionID: "e1", Argument: "{}",
	})
	if err != nil || !ok {
		t.Fatalf("exec: %v %v", ok, err)
	}
	ex, found, err := st.GetWorkflowExecution(exName)
	if err != nil || !found {
		t.Fatal(err)
	}
	exs, err := st.ListWorkflowExecutions(wfName)
	if err != nil || len(exs) < 1 || ex.Name == "" {
		t.Fatalf("list ex: %v", exs)
	}
	ok, err = st.DeleteWorkflow(wfName)
	if err != nil || !ok {
		t.Fatal(err)
	}

	orgs, err := st.ListOrganizations()
	if err != nil {
		t.Fatal(err)
	}
	_ = orgs
	ok, err = st.CreateOrganization(store.Organization{
		Name: "organizations/999", OrgID: "999", DisplayName: "Extra",
	})
	if err != nil || !ok {
		t.Fatalf("create org: %v %v", ok, err)
	}

	appID := project
	ok, err = st.CreateAppEngineApp(store.AppEngineApp{
		Name: "apps/" + appID, AppID: appID, LocationID: "us-central",
	})
	if err != nil || !ok {
		t.Fatalf("app: %v %v", ok, err)
	}
	app, found, err := st.GetAppEngineApp(appID)
	if err != nil || !found || app.AppID == "" {
		t.Fatal(err)
	}
	svc, err := st.EnsureAppEngineService(appID, "default")
	if err != nil || svc.ServiceID != "default" {
		t.Fatalf("ensure svc: %#v err=%v", svc, err)
	}
	ok, err = st.CreateAppEngineVersion(store.AppEngineVersion{
		Name: "apps/" + appID + "/services/default/versions/v1", AppID: appID, ServiceID: "default", VersionID: "v1",
	})
	if err != nil || !ok {
		t.Fatalf("version: %v %v", ok, err)
	}
	ver, found, err := st.GetAppEngineVersion(appID, "default", "v1")
	if err != nil || !found {
		t.Fatal(err)
	}
	vers, err := st.ListAppEngineVersions(appID, "default")
	if err != nil || len(vers) < 1 || ver.VersionID == "" {
		t.Fatalf("list ver: %v", vers)
	}
	svcs, err := st.ListAppEngineServices(appID)
	if err != nil || len(svcs) < 1 {
		t.Fatalf("list svc: %v", svcs)
	}
	usvc, found, err := st.UpdateAppEngineServiceTraffic(appID, "default", `{"allocations":{"v1":1}}`, "COOKIE", false)
	if err != nil || !found {
		t.Fatalf("traffic %#v err=%v", usvc, err)
	}
	aesvc, found, err := st.GetAppEngineService(appID, "default")
	if err != nil || !found || aesvc.ServiceID == "" {
		t.Fatal(err)
	}
}
