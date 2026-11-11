package restlab_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
)

func TestActAsNegative(t *testing.T) {
	const (
		projectP       = "pebble-other"
		projectQ       = "quartz-owned"
		defaultProject = "noctaxris-gcp-local"
	)
	caller := "caller@" + projectP + ".iam.gserviceaccount.com"
	homeSA := "runner@" + projectP + ".iam.gserviceaccount.com"
	foreignSA := "foreign@" + projectQ + ".iam.gserviceaccount.com"
	emailSaysP := "worker@" + projectP + ".iam.gserviceaccount.com"
	rootSA := "root@" + defaultProject + ".iam.gserviceaccount.com"
	unknown := "missing@" + projectQ + ".iam.gserviceaccount.com"
	emptyHome := "no-home@" + projectP + ".iam.gserviceaccount.com"
	blankHome := "blank-home@" + projectP + ".iam.gserviceaccount.com"
	blankAddr := "named@" + projectQ + ".iam.gserviceaccount.com"

	rows := saRows{
		homeSA:     {ProjectID: projectP, Email: homeSA},
		foreignSA:  {ProjectID: projectQ, Email: foreignSA},
		emailSaysP: {ProjectID: projectQ, Email: emailSaysP},
		rootSA:     {ProjectID: projectQ, Email: rootSA},
		emptyHome:  {ProjectID: "", Email: emptyHome},
		blankHome:  {ProjectID: "   ", Email: blankHome},
		blankAddr:  {ProjectID: projectQ, Email: "  "},
	}
	// Callers are owners of project P only. The extra bindings name foreign
	// emails under P, or the root email under the default project, so a check
	// that ignores the stored home would allow. Owner on P still covers an SA
	// whose stored home is P.
	members := []string{"serviceAccount:" + caller, "serviceAccount:" + rootSA}
	onlyOnP := &authz.Evaluator{Policies: memPolicies{
		"projects/" + projectP:                                      mustPolicyMembers(t, "roles/owner", members...),
		"projects/" + projectP + "/serviceAccounts/" + homeSA:       mustPolicyMembers(t, "roles/iam.serviceAccountUser", members...),
		"projects/" + projectP + "/serviceAccounts/" + foreignSA:    mustPolicyMembers(t, "roles/iam.serviceAccountUser", members...),
		"projects/" + projectP + "/serviceAccounts/" + emailSaysP:   mustPolicyMembers(t, "roles/iam.serviceAccountUser", members...),
		"projects/" + projectP + "/serviceAccounts/" + rootSA:       mustPolicyMembers(t, "roles/iam.serviceAccountUser", members...),
		"projects/" + projectP + "/serviceAccounts/" + unknown:      mustPolicyMembers(t, "roles/iam.serviceAccountUser", members...),
		"projects/" + projectP + "/serviceAccounts/" + emptyHome:    mustPolicyMembers(t, "roles/iam.serviceAccountUser", members...),
		"projects/" + projectP + "/serviceAccounts/" + blankHome:    mustPolicyMembers(t, "roles/iam.serviceAccountUser", members...),
		"projects/" + defaultProject + "/serviceAccounts/" + rootSA: mustPolicyMembers(t, "roles/iam.serviceAccountUser", members...),
	}}
	ownerOfQ := &authz.Evaluator{Policies: memPolicies{
		"projects/" + projectQ: mustPolicy(t, "roles/owner", "serviceAccount:"+caller),
		"projects/" + projectQ + "/serviceAccounts/" + blankAddr: mustPolicy(t, "roles/iam.serviceAccountUser", "serviceAccount:"+caller),
	}}

	callerP := authn.Principal{Email: caller, IsRoot: false}
	callerRootEmail := authn.Principal{Email: rootSA, IsRoot: false}

	t.Run("other home project", func(t *testing.T) {
		requireActAsAllowed(t, onlyOnP, rows, callerP, homeSA)
		requireActAsAllowed(t, onlyOnP, rows, callerRootEmail, homeSA)
		requireActAsDenied(t, onlyOnP, rows, callerP, foreignSA)
		requireActAsDenied(t, onlyOnP, rows, callerRootEmail, foreignSA)
		requireActAsDenied(t, onlyOnP, rows, callerP, emailSaysP)
		requireActAsDenied(t, onlyOnP, rows, callerRootEmail, emailSaysP)
	})

	t.Run("root service account email", func(t *testing.T) {
		requireActAsDenied(t, onlyOnP, rows, callerP, rootSA)
		requireActAsDenied(t, onlyOnP, rows, callerRootEmail, rootSA)
	})

	t.Run("unknown email", func(t *testing.T) {
		requireActAsDenied(t, onlyOnP, rows, callerP, unknown)
		requireActAsDenied(t, onlyOnP, rows, callerRootEmail, unknown)
		requireActAsDenied(t, onlyOnP, rows, callerP, "not-in-the-map")
	})

	t.Run("empty email", func(t *testing.T) {
		for _, email := range []string{"", "   ", "\t\n"} {
			requireActAsDenied(t, onlyOnP, rows, callerP, email)
			requireActAsDenied(t, onlyOnP, rows, callerRootEmail, email)
			requireActAsDenied(t, onlyOnP, rows, authn.Principal{Email: caller, IsRoot: true}, email)
			requireActAsDenied(t, ownerOfQ, rows, callerP, email)
		}
	})

	t.Run("empty home or stored email", func(t *testing.T) {
		requireActAsDenied(t, onlyOnP, rows, callerP, emptyHome)
		requireActAsDenied(t, onlyOnP, rows, callerRootEmail, blankHome)
		requireActAsDenied(t, ownerOfQ, rows, callerP, blankAddr)
	})
}

func mustPolicyMembers(t *testing.T, role string, members ...string) []byte {
	t.Helper()
	b, err := json.Marshal(authz.Policy{Bindings: []authz.Binding{{Role: role, Members: members}}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func requireActAsAllowed(t *testing.T, ev *authz.Evaluator, rows saRows, p authn.Principal, email string) {
	t.Helper()
	ok, err := restlab.CheckServiceAccountActAs(ev, rows, p, email)
	if err != nil || !ok {
		t.Fatalf("check allow email=%q caller=%q ok=%v err=%v", email, p.Email, ok, err)
	}
	rec := httptest.NewRecorder()
	if !restlab.RequireServiceAccountActAs(rec, ev, rows, p, email) {
		t.Fatalf("require allow email=%q caller=%q body=%s", email, p.Email, rec.Body.String())
	}
}

func requireActAsDenied(t *testing.T, ev *authz.Evaluator, rows saRows, p authn.Principal, email string) {
	t.Helper()
	ok, err := restlab.CheckServiceAccountActAs(ev, rows, p, email)
	if err != nil || ok {
		t.Fatalf("check deny email=%q caller=%q ok=%v err=%v", email, p.Email, ok, err)
	}
	rec := httptest.NewRecorder()
	if restlab.RequireServiceAccountActAs(rec, ev, rows, p, email) {
		t.Fatalf("require allowed email=%q caller=%q", email, p.Email)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("email=%q caller=%q status=%d body=%s", email, p.Email, rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Status string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("email=%q body=%s err=%v", email, rec.Body.String(), err)
	}
	if body.Error.Status != "PERMISSION_DENIED" {
		t.Fatalf("email=%q body=%s", email, rec.Body.String())
	}
}
