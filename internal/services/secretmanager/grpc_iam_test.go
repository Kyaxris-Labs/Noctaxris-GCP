package secretmanager_test

import (
	"testing"

	iampb "cloud.google.com/go/iam/apiv1/iampb"
	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
)

func TestSecretManagerGRPCIamPolicy(t *testing.T) {
	client, ctx, cleanup := startSecretManagerGRPC(t)
	defer cleanup()
	project := "noctaxris-gcp-local"
	parent := "projects/" + project

	sec, err := client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{
		Parent:   parent,
		SecretId: "iam-sec",
		Secret:   &secretmanagerpb.Secret{},
	})
	if err != nil {
		t.Fatal(err)
	}
	name := sec.GetName()

	pol, err := client.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Resource: name})
	if err != nil {
		t.Fatal(err)
	}
	if string(pol.GetEtag()) == "" && len(pol.GetBindings()) != 0 {
		t.Fatalf("empty policy unexpected: %#v", pol)
	}

	set, err := client.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{
		Resource: name,
		Policy: &iampb.Policy{
			Etag: []byte("ACAB"),
			Bindings: []*iampb.Binding{{
				Role:    "roles/secretmanager.secretAccessor",
				Members: []string{"serviceAccount:reader@example.com"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(set.GetBindings()) != 1 {
		t.Fatalf("set bindings=%v", set.GetBindings())
	}

	got, err := client.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Resource: name})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.GetBindings()) != 1 || got.GetBindings()[0].GetRole() != "roles/secretmanager.secretAccessor" {
		t.Fatalf("get after set=%#v", got)
	}

	perms, err := client.TestIamPermissions(ctx, &iampb.TestIamPermissionsRequest{
		Resource:    name,
		Permissions: []string{"secretmanager.versions.access", "secretmanager.secrets.get"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(perms.GetPermissions()) == 0 {
		t.Fatalf("expected root-granted permissions, got %v", perms.GetPermissions())
	}
}
