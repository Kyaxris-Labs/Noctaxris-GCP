package store

import (
	"path/filepath"
	"testing"
)

func TestStoreZeroCoverageHelpers(t *testing.T) {
	dir := t.TempDir()
	key, err := LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	project := "noctaxris-gcp-local"
	if err := st.EnsureRoot(project, "root@"+project+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	if err := st.Ping(); err != nil {
		t.Fatal(err)
	}
	if st.DataRoot() == "" {
		t.Fatal("empty DataRoot")
	}

	sec := "projects/" + project + "/secrets/z-sec"
	if _, created, err := st.CreateSecret(sec, project); err != nil || !created {
		t.Fatalf("create secret: %v created=%v", err, created)
	}
	list, err := st.ListSecrets(project)
	if err != nil || len(list) < 1 {
		t.Fatalf("list secrets: n=%d err=%v", len(list), err)
	}
	if _, err := st.AddSecretVersion(sec, []byte("v")); err != nil {
		t.Fatal(err)
	}
	name, ver, ok := ParseSecretVersionName(sec + "/versions/1")
	if !ok || name != sec || ver != "1" {
		t.Fatalf("parse version: name=%q ver=%q ok=%v", name, ver, ok)
	}
	deleted, err := st.DeleteSecret(sec)
	if err != nil || !deleted {
		t.Fatalf("delete secret: %v deleted=%v", deleted, err)
	}

	armorName := CloudArmorPolicyResourceName(project, "z-armor")
	created, err := st.CreateCloudArmorSecurityPolicy(CloudArmorSecurityPolicy{
		Name: armorName, ProjectID: project, PolicyID: "z-armor",
		RulesJSON: DefaultCloudArmorRulesJSON(), BodyJSON: `{}`,
	})
	if err != nil || !created {
		t.Fatalf("create armor: %v created=%v", err, created)
	}
	got, ok, err := st.GetCloudArmorSecurityPolicyByProjectID(project, "z-armor")
	if err != nil || !ok || got.PolicyID != "z-armor" {
		t.Fatalf("get armor by project: %#v ok=%v err=%v", got, ok, err)
	}
	_, ok, err = st.UpdateCloudArmorSecurityPolicyRules(armorName, DefaultCloudArmorRulesJSON(), "d")
	if err != nil || !ok {
		t.Fatalf("update armor rules: ok=%v err=%v", ok, err)
	}
	_, ok, err = st.UpdateCloudArmorSecurityPolicyBody(armorName, `{"labels":{"a":"1"}}`)
	if err != nil || !ok {
		t.Fatalf("update armor body: ok=%v err=%v", ok, err)
	}

	mapName := "projects/" + project + "/locations/global/certificateMaps/z-map"
	created, err = st.CreateCertManagerCertificateMap(CertManagerCertificateMap{
		Name: mapName, ProjectID: project, Location: "global", MapID: "z-map", Description: "z",
	})
	if err != nil || !created {
		t.Fatalf("create cert map: %v created=%v", err, created)
	}
	cm, ok, err := st.GetCertManagerCertificateMap(mapName)
	if err != nil || !ok || cm.MapID != "z-map" {
		t.Fatalf("get cert map: %#v ok=%v err=%v", cm, ok, err)
	}

	parent := DefaultOrganizationName
	srcName := SCCSourceResourceName(parent, "zsrc")
	created, err = st.CreateSCCSource(SCCSource{
		Name: srcName, Parent: parent, SourceID: "zsrc", DisplayName: "Z",
	})
	if err != nil || !created {
		t.Fatalf("create source: %v created=%v", err, created)
	}
	fName := SCCFindingResourceName(srcName, "zf1")
	created, err = st.CreateSCCFinding(SCCFinding{
		Name: fName, Parent: parent, SourceName: srcName, FindingID: "zf1",
		Category: "THREAT", State: "ACTIVE", Severity: "HIGH",
		ResourceName: "projects/" + project,
	})
	if err != nil || !created {
		t.Fatalf("create finding: %v created=%v", err, created)
	}
	del, err := st.DeleteSCCFinding(fName)
	if err != nil || !del {
		t.Fatalf("delete finding: %v err=%v", del, err)
	}

	_ = lbTargetHTTPSProxyName(project, "us-central1", "p1")
	_ = lbTargetHTTPSProxyName(project, "", "p1")
	b, o, ok := storageSourceFromConfigJSON(`{"buildConfig":{"source":{"storageSource":{"bucket":"b","object":"o.zip"}}}}`)
	if !ok || b != "b" || o != "o.zip" {
		t.Fatalf("storage source: %q %q ok=%v", b, o, ok)
	}
	_, _, ok = storageSourceFromConfigJSON(`{}`)
	if ok {
		t.Fatal("expected no storage source")
	}

	wfName := "projects/" + project + "/locations/us-central1/workflows/z-wf"
	created, err = st.CreateWorkflow(Workflow{
		Name: wfName, ProjectID: project, Location: "us-central1", WorkflowID: "z-wf",
		SourceContents: "main: return 1",
	})
	if err != nil || !created {
		t.Fatalf("create workflow: %v created=%v", err, created)
	}
	page, token, err := st.ListWorkflowsPageDeepen(project, "us-central1", 10, "")
	if err != nil || len(page) < 1 {
		t.Fatalf("list workflows page: n=%d token=%q err=%v", len(page), token, err)
	}
	exName := wfName + "/executions/e1"
	created, err = st.CreateWorkflowExecution(WorkflowExecution{
		Name: exName, WorkflowName: wfName, ProjectID: project,
		Location: "us-central1", WorkflowID: "z-wf", ExecutionID: "e1", State: "ACTIVE",
	})
	if err != nil || !created {
		t.Fatalf("create exec: created=%v err=%v", created, err)
	}
	adv, ok, err := st.AdvanceWorkflowExecutionDeepen(exName)
	if err != nil || !ok {
		t.Fatalf("advance: %#v ok=%v err=%v", adv, ok, err)
	}
}
