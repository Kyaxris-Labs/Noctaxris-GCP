package store

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/audit"
)

func openCloudAuditStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	key, err := LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.migrateCloudAudit(); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestCloudAuditWriteList(t *testing.T) {
	st := openCloudAuditStore(t)
	project := "noctaxris-gcp-local"
	logName := CloudAuditLogName(project, CloudAuditLogIDActivity)
	proto := `{"@type":"` + CloudAuditProtoPayloadType + `","serviceName":"storage.googleapis.com","methodName":"storage.objects.get","resourceName":"projects/_/buckets/b/objects/o","authenticationInfo":{"principalEmail":"alice@example.com"}}`
	if err := st.WriteCloudAuditEntries([]CloudAuditEntry{{
		InsertID: "cal-1", ProjectID: project, LogName: logName,
		Severity: "NOTICE", Timestamp: "2026-07-30T00:00:00Z",
		ProtoPayloadJSON: proto, ResourceJSON: `{"type":"gcs_bucket"}`,
	}}); err != nil {
		t.Fatal(err)
	}

	rows, err := st.ListCloudAuditEntries(ListCloudAuditFilter{
		ProjectID: project, ExactLogName: logName, PageSize: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].MethodName != "storage.objects.get" {
		t.Fatalf("rows=%#v", rows)
	}

	asLog, err := st.ListCloudAuditAsLogEntries(ListCloudAuditFilter{
		ProjectID: project, ExactLogName: logName, PageSize: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(asLog) != 1 || !IsCloudAuditLogName(asLog[0].LogName) {
		t.Fatalf("asLog=%#v", asLog)
	}
	if !strings.Contains(asLog[0].PayloadJSON, "protoPayload") {
		t.Fatalf("payload=%s", asLog[0].PayloadJSON)
	}
}

func TestCloudAuditFromKernelEvent(t *testing.T) {
	st := openCloudAuditStore(t)
	project := "noctaxris-gcp-local"
	granted := true
	err := st.WriteCloudAuditFromKernelEvent(project, audit.Event{
		InsertID: "live-1", Timestamp: time.Date(2026, 7, 30, 1, 0, 0, 0, time.UTC),
		PrincipalEmail: "root@lab", MethodName: "google.iam.v1.TestIamPermissions",
		ResourceName: "projects/" + project, Permission: "resourcemanager.projects.get",
		Granted: &granted, ServiceName: "cloudresourcemanager.googleapis.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	names, err := st.ListCloudAuditLogNames(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 {
		t.Fatalf("names=%v", names)
	}
}
