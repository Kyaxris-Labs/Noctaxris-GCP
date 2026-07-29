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

func TestTokenCreatorGrantsGetAccessToken(t *testing.T) {
	saResource := "projects/noctaxris-gcp-local/serviceAccounts/target@noctaxris-gcp-local.iam.gserviceaccount.com"
	caller := "caller@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			saResource: mustPolicy(t, "roles/iam.serviceAccountTokenCreator", "serviceAccount:"+caller),
		},
	}
	for _, perm := range []string{
		"iam.serviceAccounts.getAccessToken",
		"iam.serviceAccounts.actAs",
		"iam.serviceAccounts.signBlob",
		"iam.serviceAccounts.signJwt",
		"iam.serviceAccounts.generateAccessToken",
		"iam.serviceAccounts.generateIdToken",
	} {
		ok, err := e.Evaluate(caller, false, perm, saResource)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("TokenCreator should grant %s", perm)
		}
	}
	// TokenCreator is not a general SA admin role.
	ok, err := e.Evaluate(caller, false, "iam.serviceAccounts.delete", saResource)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("TokenCreator must not grant iam.serviceAccounts.delete")
	}
}

func TestViewerAndEditorDenyGetAccessToken(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	for _, role := range []string{"roles/viewer", "roles/editor"} {
		email := "user@noctaxris-gcp-local.iam.gserviceaccount.com"
		e := &authz.Evaluator{
			Policies: memPolicies{
				resource: mustPolicy(t, role, "serviceAccount:"+email),
			},
		}
		ok, err := e.Evaluate(email, false, "iam.serviceAccounts.getAccessToken", resource)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatalf("%s must deny getAccessToken", role)
		}
	}
}

func TestRunAndFunctionsInvokerRoles(t *testing.T) {
	email := "invoker@example.com"
	svc := "projects/noctaxris-gcp-local/locations/us-central1/services/demo"
	fn := "projects/noctaxris-gcp-local/locations/us-central1/functions/fn1"
	e := &authz.Evaluator{
		Policies: memPolicies{
			svc: mustPolicy(t, "roles/run.invoker", "serviceAccount:"+email),
			fn:  mustPolicy(t, "roles/cloudfunctions.invoker", "serviceAccount:"+email),
		},
	}
	ok, err := e.Evaluate(email, false, "run.routes.invoke", svc)
	if err != nil || !ok {
		t.Fatalf("run.invoker should grant run.routes.invoke: ok=%v err=%v", ok, err)
	}
	ok, err = e.Evaluate(email, false, "run.services.create", svc)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("roles/run.invoker must not grant run.services.create")
	}
	ok, err = e.Evaluate(email, false, "cloudfunctions.functions.invoke", fn)
	if err != nil || !ok {
		t.Fatalf("cloudfunctions.invoker should grant invoke: ok=%v err=%v", ok, err)
	}
	ok, err = e.Evaluate(email, false, "cloudfunctions.functions.delete", fn)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("roles/cloudfunctions.invoker must not grant delete")
	}
}

type memParents map[string]string

func (m memParents) CRMParent(resource string) (string, bool, error) {
	p, ok := m[resource]
	return p, ok && p != "", nil
}

func TestEvaluateOrgIAMGrantsProjectPermission(t *testing.T) {
	org := "organizations/noctaxris-gcp-org"
	project := "projects/noctaxris-gcp-local"
	email := "org-viewer@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			org: mustPolicy(t, "roles/viewer", "serviceAccount:"+email),
		},
		Parents: memParents{
			project: org,
		},
	}
	ok, err := e.Evaluate(email, false, "resourcemanager.projects.get", project)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("org viewer binding should grant project get via CRM inheritance")
	}
	ok, err = e.Evaluate("other@noctaxris-gcp-local.iam.gserviceaccount.com", false, "resourcemanager.projects.get", project)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("unbound principal must still deny")
	}
}

func TestEvaluateFolderIAMGrantsProjectPermission(t *testing.T) {
	org := "organizations/noctaxris-gcp-org"
	folder := "folders/team-a"
	project := "projects/noctaxris-gcp-local"
	email := "folder-editor@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			folder: mustPolicy(t, "roles/editor", "serviceAccount:"+email),
		},
		Parents: memParents{
			project: folder,
			folder:  org,
		},
	}
	ok, err := e.Evaluate(email, false, "storage.buckets.create", project)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("folder editor binding should grant project mutate via CRM inheritance")
	}
}

func TestEvaluateFolderInheritsOrgIAM(t *testing.T) {
	org := "organizations/noctaxris-gcp-org"
	folder := "folders/team-a"
	email := "org-owner@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			org: mustPolicy(t, "roles/owner", "serviceAccount:"+email),
		},
		Parents: memParents{
			folder: org,
		},
	}
	ok, err := e.Evaluate(email, false, "resourcemanager.folders.get", folder)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("org owner binding should grant folder get via CRM inheritance")
	}
}

func TestEvaluateNestedResourceInheritsOrgIAM(t *testing.T) {
	org := "organizations/noctaxris-gcp-org"
	project := "projects/noctaxris-gcp-local"
	sa := project + "/serviceAccounts/app@noctaxris-gcp-local.iam.gserviceaccount.com"
	email := "org-viewer@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			org: mustPolicy(t, "roles/viewer", "serviceAccount:"+email),
		},
		Parents: memParents{
			project: org,
		},
	}
	ok, err := e.Evaluate(email, false, "iam.serviceAccounts.get", sa)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("org viewer should grant nested SA get via project then org chain")
	}
}

func TestEvaluateWithoutParentsSkipsOrg(t *testing.T) {
	org := "organizations/noctaxris-gcp-org"
	project := "projects/noctaxris-gcp-local"
	email := "org-viewer@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			org: mustPolicy(t, "roles/viewer", "serviceAccount:"+email),
		},
	}
	ok, err := e.Evaluate(email, false, "resourcemanager.projects.get", project)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("without Parents, org binding must not apply to project")
	}
}

type memRoles map[string][]string

func (m memRoles) GetRoleIncludedPermissions(roleName string) ([]string, bool, error) {
	p, ok := m[roleName]
	return p, ok, nil
}

func TestCustomRoleIncludedPermissions(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "sa@noctaxris-gcp-local.iam.gserviceaccount.com"
	customRole := "projects/noctaxris-gcp-local/roles/bucketLister"
	e := &authz.Evaluator{
		Policies: memPolicies{
			resource: mustPolicy(t, customRole, "serviceAccount:"+email),
		},
		Roles: memRoles{
			customRole: []string{"storage.buckets.list", "storage.objects.get"},
		},
	}
	ok, err := e.Evaluate(email, false, "storage.buckets.list", resource)
	if err != nil || !ok {
		t.Fatalf("custom role should grant included permission: ok=%v err=%v", ok, err)
	}
	ok, err = e.Evaluate(email, false, "storage.buckets.create", resource)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("custom role must not grant permission outside includedPermissions")
	}
}

func TestUnknownRoleNoLongerOverGrants(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "sa@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			resource: mustPolicy(t, "roles/xyz.admin", "serviceAccount:"+email),
		},
	}
	ok, err := e.Evaluate(email, false, "xyz.widgets.create", resource)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("unknown roles/xyz.* must not grant via prefix heuristic")
	}
	ok, err = e.Evaluate(email, false, "storage.buckets.create", resource)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("unknown role must not grant unrelated permissions")
	}
}
