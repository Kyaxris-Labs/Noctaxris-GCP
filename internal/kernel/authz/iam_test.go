package authz_test

import (
	"encoding/json"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
)

type memPolicies map[string][]byte

func (m memPolicies) GetIAMPolicyJSON(resource string) ([]byte, bool, error) {
	b, ok := m[resource]
	return b, ok, nil
}

func mustPolicy(t *testing.T, role, member string) []byte {
	t.Helper()
	p := authz.Policy{
		Bindings: []authz.Binding{{Role: role, Members: []string{member}}},
		Etag:     "etag1",
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEvaluateRootBypass(t *testing.T) {
	e := &authz.Evaluator{Policies: memPolicies{}}
	ok, err := e.Evaluate("root@noctaxris-gcp-local.iam.gserviceaccount.com", true, "storage.buckets.create", "projects/noctaxris-gcp-local")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("root must bypass")
	}
}

func TestEvaluateAllowDeny(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "sa@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			resource: mustPolicy(t, "roles/owner", "serviceAccount:"+email),
		},
	}
	ok, err := e.Evaluate(email, false, "storage.buckets.create", resource)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("owner binding should allow")
	}
	ok, err = e.Evaluate("other@noctaxris-gcp-local.iam.gserviceaccount.com", false, "storage.buckets.create", resource)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("unbound principal should deny")
	}
}

func TestEvaluateDenyByDefaultMissingPolicy(t *testing.T) {
	e := &authz.Evaluator{Policies: memPolicies{}}
	ok, err := e.Evaluate("sa@noctaxris-gcp-local.iam.gserviceaccount.com", false, "storage.buckets.create", "projects/missing")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("missing policy must deny")
	}
}

func TestEvaluateParentProjectInheritance(t *testing.T) {
	project := "projects/noctaxris-gcp-local"
	sa := project + "/serviceAccounts/app@noctaxris-gcp-local.iam.gserviceaccount.com"
	email := "owner@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			project: mustPolicy(t, "roles/owner", "serviceAccount:"+email),
		},
	}
	ok, err := e.Evaluate(email, false, "iam.serviceAccounts.get", sa)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("project owner binding should grant nested SA permission")
	}
}

func TestEvaluateAnyBucketOrProject(t *testing.T) {
	email := "sa@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			"buckets/lab": mustPolicy(t, "roles/storage.objectViewer", "serviceAccount:"+email),
		},
	}
	ok, err := e.EvaluateAny(email, false, "storage.objects.get", "buckets/lab", "projects/noctaxris-gcp-local")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("bucket policy should allow via EvaluateAny")
	}
	ok, err = e.EvaluateAny(email, false, "storage.objects.get", "buckets/other", "projects/noctaxris-gcp-local")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("missing policies must deny")
	}
}

func TestTestIamPermissions(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "viewer@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			resource: mustPolicy(t, "roles/viewer", "serviceAccount:"+email),
		},
	}
	got, err := e.TestIamPermissions(email, false, resource, []string{
		"resourcemanager.projects.get",
		"storage.buckets.create",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "resourcemanager.projects.get" {
		t.Fatalf("got %v", got)
	}
}

func TestViewerGrantsCommonServiceReads(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "viewer@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			resource: mustPolicy(t, "roles/viewer", "serviceAccount:"+email),
		},
	}
	reads := []string{
		"storage.objects.get",
		"storage.objects.list",
		"pubsub.topics.list",
		"secretmanager.secrets.get",
		"secretmanager.versions.get",
		"cloudkms.cryptoKeys.get",
		"logging.logEntries.list",
		"serviceusage.services.get",
		"iam.serviceAccounts.list",
		"resourcemanager.projects.search",
	}
	for _, perm := range reads {
		ok, err := e.Evaluate(email, false, perm, resource)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("viewer should grant %s", perm)
		}
	}
	denied := []string{
		"storage.buckets.create",
		"secretmanager.versions.access",
		"iam.serviceAccounts.getAccessToken",
		"bigquery.tables.getData",
	}
	for _, perm := range denied {
		ok, err := e.Evaluate(email, false, perm, resource)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatalf("viewer must not grant %s", perm)
		}
	}
}

func TestEditorDeniesIAMAdminAndImpersonation(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "editor@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			resource: mustPolicy(t, "roles/editor", "serviceAccount:"+email),
		},
	}
	ok, err := e.Evaluate(email, false, "storage.buckets.create", resource)
	if err != nil || !ok {
		t.Fatalf("editor should create buckets: ok=%v err=%v", ok, err)
	}
	for _, perm := range []string{
		"resourcemanager.projects.setIamPolicy",
		"iam.serviceAccounts.getAccessToken",
		"iam.serviceAccounts.signBlob",
	} {
		ok, err := e.Evaluate(email, false, perm, resource)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatalf("editor must not grant %s", perm)
		}
	}
}

func TestNilEvaluatorFailClosed(t *testing.T) {
	var e *authz.Evaluator
	ok, err := e.Evaluate("a@b.c", false, "storage.objects.get", "projects/p")
	if err != nil || ok {
		t.Fatalf("nil evaluator must deny non-root: ok=%v err=%v", ok, err)
	}
	ok, err = e.Evaluate("a@b.c", true, "storage.objects.get", "projects/p")
	if err != nil || !ok {
		t.Fatalf("nil evaluator still allows root: ok=%v err=%v", ok, err)
	}
}
