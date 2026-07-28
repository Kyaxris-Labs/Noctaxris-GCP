package store_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestStripTaskAuthTokens(t *testing.T) {
	in := `{"url":"http://example","oidcToken":{"serviceAccountEmail":"a"},"oauthToken":{"serviceAccountEmail":"b"},"httpMethod":"POST"}`
	out := store.StripTaskAuthTokens(in)
	if strings.Contains(out, "oidcToken") || strings.Contains(out, "oauthToken") {
		t.Fatalf("tokens not stripped: %s", out)
	}
	if !strings.Contains(out, "http://example") {
		t.Fatalf("url lost: %s", out)
	}
}

func TestServerlessStoreCRUD(t *testing.T) {
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

	created, err := st.CreateRunService(store.RunService{
		Name: "projects/p/locations/us-central1/services/s1", ProjectID: "p", Location: "us-central1", ServiceID: "s1",
		TemplateJSON: `{"containers":[]}`,
	})
	if err != nil || !created {
		t.Fatalf("create run: created=%v err=%v", created, err)
	}
	revs, err := st.ListRunRevisions("projects/p/locations/us-central1/services/s1")
	if err != nil || len(revs) != 1 {
		t.Fatalf("revs=%v err=%v", revs, err)
	}

	fnOK, err := st.CreateCloudFunction(store.CloudFunction{
		Name: "projects/p/locations/us-central1/functions/f1", ProjectID: "p", Location: "us-central1", FunctionID: "f1",
	})
	if err != nil || !fnOK {
		t.Fatalf("create fn: %v %v", fnOK, err)
	}

	jobOK, err := st.CreateSchedulerJob(store.SchedulerJob{
		Name: "projects/p/locations/us-central1/jobs/j1", ProjectID: "p", Location: "us-central1", JobID: "j1",
		Schedule: "* * * * *",
	})
	if err != nil || !jobOK {
		t.Fatalf("create job: %v %v", jobOK, err)
	}

	qOK, err := st.CreateCloudTasksQueue(store.CloudTasksQueue{
		Name: "projects/p/locations/us-central1/queues/q1", ProjectID: "p", Location: "us-central1", QueueID: "q1",
	})
	if err != nil || !qOK {
		t.Fatalf("create queue: %v %v", qOK, err)
	}
	tOK, err := st.CreateCloudTask(store.CloudTask{
		Name:            "projects/p/locations/us-central1/queues/q1/tasks/t1",
		QueueName:       "projects/p/locations/us-central1/queues/q1",
		HTTPRequestJSON: `{"url":"http://x","oidcToken":{"serviceAccountEmail":"sa"}}`,
	})
	if err != nil || !tOK {
		t.Fatalf("create task: %v %v", tOK, err)
	}
	task, ok, err := st.GetCloudTask("projects/p/locations/us-central1/queues/q1/tasks/t1")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if strings.Contains(task.HTTPRequestJSON, "oidcToken") {
		t.Fatalf("oidc stored: %s", task.HTTPRequestJSON)
	}

	next := store.NextCronRunRFC3339("0 9 * * 1", "UTC", time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC))
	if next == "" {
		t.Fatal("expected next cron run")
	}
	cleaned, aud := store.StripSchedulerOIDC(`{"uri":"http://x","oidcToken":{"audience":"aud1","serviceAccountEmail":"sa"}}`)
	if aud != "aud1" || strings.Contains(cleaned, "oidcToken") {
		t.Fatalf("strip oidc: cleaned=%s aud=%s", cleaned, aud)
	}
}
