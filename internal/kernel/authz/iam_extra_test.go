package authz_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
)

func TestEvaluateEmptyInputsAndNilPolicies(t *testing.T) {
	e := &authz.Evaluator{Policies: memPolicies{}}
	cases := []struct {
		email, perm, res string
	}{
		{"", "storage.buckets.get", "projects/p"},
		{"sa@p.iam.gserviceaccount.com", "", "projects/p"},
		{"sa@p.iam.gserviceaccount.com", "storage.buckets.get", ""},
	}
	for _, tc := range cases {
		ok, err := e.Evaluate(tc.email, false, tc.perm, tc.res)
		if err != nil || ok {
			t.Fatalf("empty input must deny: %+v ok=%v err=%v", tc, ok, err)
		}
	}
	ok, err := (&authz.Evaluator{}).Evaluate("sa@p.iam.gserviceaccount.com", false, "storage.buckets.get", "projects/p")
	if err != nil || ok {
		t.Fatalf("nil Policies must deny: ok=%v err=%v", ok, err)
	}
}

func TestEvaluateAnyNilAndEmptyResources(t *testing.T) {
	ok, err := (*authz.Evaluator)(nil).EvaluateAny("sa@x", false, "storage.buckets.get", "projects/p")
	if err != nil || ok {
		t.Fatalf("nil evaluator EvaluateAny must deny: ok=%v err=%v", ok, err)
	}
	e := &authz.Evaluator{Policies: memPolicies{
		"projects/p": mustPolicy(t, "roles/viewer", "serviceAccount:sa@p.iam.gserviceaccount.com"),
	}}
	ok, err = e.EvaluateAny("sa@p.iam.gserviceaccount.com", false, "resourcemanager.projects.get", "", "projects/p")
	if err != nil || !ok {
		t.Fatalf("skip empty resource then allow: ok=%v err=%v", ok, err)
	}
	ok, err = e.EvaluateAny("root@x", true, "storage.buckets.get")
	if err != nil || !ok {
		t.Fatalf("root EvaluateAny must allow: ok=%v err=%v", ok, err)
	}
}

func TestConditionAllowsAndDenies(t *testing.T) {
	email := "sa@noctaxris-gcp-local.iam.gserviceaccount.com"
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	pol := authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/viewer",
			Members: []string{"serviceAccount:" + email},
			Condition: &authz.Expr{
				Expression: `request.time < timestamp("2026-10-03T11:00:00Z")`,
			},
		}},
	}
	raw, _ := json.Marshal(pol)
	e := &authz.Evaluator{
		Policies: memPolicies{"projects/p": raw},
		Now:      func() time.Time { return now },
	}
	ok, err := e.Evaluate(email, false, "resourcemanager.projects.get", "projects/p")
	if err != nil || ok {
		t.Fatalf("expired condition window must deny: ok=%v err=%v", ok, err)
	}

	pol.Bindings[0].Condition.Expression = `request.time < timestamp("2026-10-03T13:00:00Z")`
	raw, _ = json.Marshal(pol)
	e.Policies = memPolicies{"projects/p": raw}
	ok, err = e.Evaluate(email, false, "resourcemanager.projects.get", "projects/p")
	if err != nil || !ok {
		t.Fatalf("open condition window must allow: ok=%v err=%v", ok, err)
	}
}

func TestUnknownRoleFailClosedAndPrefixedMember(t *testing.T) {
	email := "sa@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{Policies: memPolicies{
		"projects/p": mustPolicy(t, "roles/not.a.real.role", "serviceAccount:"+email),
	}}
	ok, err := e.Evaluate(email, false, "storage.buckets.get", "projects/p")
	if err != nil || ok {
		t.Fatalf("unknown role must deny: ok=%v err=%v", ok, err)
	}
	// Member already prefixed.
	e.Policies = memPolicies{
		"projects/p": mustPolicy(t, "roles/viewer", "user:alice@example.com"),
	}
	ok, err = e.Evaluate("user:alice@example.com", false, "resourcemanager.projects.get", "projects/p")
	if err != nil || !ok {
		t.Fatalf("prefixed member must match: ok=%v err=%v", ok, err)
	}
}

type errPolicies struct{}

func (errPolicies) GetIAMPolicyJSON(string) ([]byte, bool, error) {
	return nil, false, errors.New("policy store boom")
}

func TestEvaluatePolicyStoreError(t *testing.T) {
	e := &authz.Evaluator{Policies: errPolicies{}}
	ok, err := e.Evaluate("sa@x", false, "storage.buckets.get", "projects/p")
	if err == nil || ok {
		t.Fatalf("store error must surface: ok=%v err=%v", ok, err)
	}
}

func TestIsCRMHierarchyResourceBoundaries(t *testing.T) {
	email := "sa@noctaxris-gcp-local.iam.gserviceaccount.com"
	project := "projects/p"
	folder := "folders/f1"
	org := "organizations/o1"
	e := &authz.Evaluator{
		Policies: memPolicies{
			org: mustPolicy(t, "roles/viewer", "serviceAccount:"+email),
		},
		Parents: memParents{
			project: folder,
			folder:  org,
		},
	}
	ok, err := e.Evaluate(email, false, "resourcemanager.projects.get", "projects/p/locations/us/services/x")
	if err != nil || !ok {
		t.Fatalf("nested under project should walk to org: ok=%v err=%v", ok, err)
	}
}

func TestEditorViewerSecretAndKMSBoundaries(t *testing.T) {
	email := "sa@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{Policies: memPolicies{
		"projects/p": mustPolicy(t, "roles/editor", "serviceAccount:"+email),
	}}
	ok, err := e.Evaluate(email, false, "storage.objects.create", "projects/p")
	if err != nil || !ok {
		t.Fatalf("editor should allow object create: ok=%v err=%v", ok, err)
	}
	ok, err = e.Evaluate(email, false, "secretmanager.versions.access", "projects/p")
	if err != nil || ok {
		t.Fatalf("editor must not access secret payload: ok=%v err=%v", ok, err)
	}
	ok, err = e.Evaluate(email, false, "iam.serviceAccounts.actAs", "projects/p")
	if err != nil || ok {
		t.Fatalf("editor must not actAs: ok=%v err=%v", ok, err)
	}

	e.Policies = memPolicies{"projects/p": mustPolicy(t, "roles/viewer", "serviceAccount:"+email)}
	ok, err = e.Evaluate(email, false, "storage.objects.get", "projects/p")
	if err != nil || !ok {
		t.Fatalf("viewer should get objects: ok=%v err=%v", ok, err)
	}
	ok, err = e.Evaluate(email, false, "storage.objects.create", "projects/p")
	if err != nil || ok {
		t.Fatalf("viewer must not create objects: ok=%v err=%v", ok, err)
	}

	e.Policies = memPolicies{"projects/p": mustPolicy(t, "roles/cloudkms.cryptoKeyEncrypterDecrypter", "serviceAccount:"+email)}
	ok, err = e.Evaluate(email, false, "cloudkms.cryptoKeyVersions.useToEncrypt", "projects/p")
	if err != nil || !ok {
		t.Fatalf("encrypterDecrypter encrypt: ok=%v err=%v", ok, err)
	}
	ok, err = e.Evaluate(email, false, "cloudkms.cryptoKeys.create", "projects/p")
	if err != nil || ok {
		t.Fatalf("encrypterDecrypter must not create keys: ok=%v err=%v", ok, err)
	}
}

func TestInvokerAndTokenCreatorRoles(t *testing.T) {
	email := "sa@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{Policies: memPolicies{
		"projects/p": mustPolicy(t, "roles/run.invoker", "serviceAccount:"+email),
	}}
	ok, err := e.Evaluate(email, false, "run.routes.invoke", "projects/p")
	if err != nil || !ok {
		t.Fatalf("run.invoker: ok=%v err=%v", ok, err)
	}
	ok, err = e.Evaluate(email, false, "run.services.get", "projects/p")
	if err != nil || ok {
		t.Fatalf("run.invoker must not get services: ok=%v err=%v", ok, err)
	}

	e.Policies = memPolicies{"projects/p": mustPolicy(t, "roles/iam.serviceAccountTokenCreator", "serviceAccount:"+email)}
	ok, err = e.Evaluate(email, false, "iam.serviceAccounts.getAccessToken", "projects/p")
	if err != nil || !ok {
		t.Fatalf("tokenCreator getAccessToken: ok=%v err=%v", ok, err)
	}
	ok, err = e.Evaluate(email, false, "storage.buckets.get", "projects/p")
	if err != nil || ok {
		t.Fatalf("tokenCreator must not grant storage: ok=%v err=%v", ok, err)
	}
}
