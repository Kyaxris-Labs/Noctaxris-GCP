package authz_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
)

func TestSecretManagerViewerDeniesAccessVersion(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "meta-reader@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			resource: mustPolicy(t, "roles/secretmanager.viewer", "serviceAccount:"+email),
		},
	}
	ok, err := e.Evaluate(email, false, "secretmanager.secrets.get", resource)
	if err != nil || !ok {
		t.Fatalf("viewer get: ok=%v err=%v", ok, err)
	}
	ok, err = e.Evaluate(email, false, "secretmanager.versions.access", resource)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("secretmanager.viewer must not grant versions.access")
	}
}

func TestSecretManagerSecretAccessorAllowsAccessVersion(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "accessor@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			resource: mustPolicy(t, "roles/secretmanager.secretAccessor", "serviceAccount:"+email),
		},
	}
	ok, err := e.Evaluate(email, false, "secretmanager.versions.access", resource)
	if err != nil || !ok {
		t.Fatalf("accessor access: ok=%v err=%v", ok, err)
	}
}

func TestCloudKMSViewerDeniesDecrypt(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "kms-viewer@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			resource: mustPolicy(t, "roles/cloudkms.viewer", "serviceAccount:"+email),
		},
	}
	ok, err := e.Evaluate(email, false, "cloudkms.cryptoKeys.get", resource)
	if err != nil || !ok {
		t.Fatalf("viewer get: ok=%v err=%v", ok, err)
	}
	ok, err = e.Evaluate(email, false, "cloudkms.cryptoKeyVersions.useToDecrypt", resource)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("cloudkms.viewer must not grant useToDecrypt")
	}
}

func TestCloudKMSAdminDeniesCryptoOps(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "kms-admin@noctaxris-gcp-local.iam.gserviceaccount.com"
	e := &authz.Evaluator{
		Policies: memPolicies{
			resource: mustPolicy(t, "roles/cloudkms.admin", "serviceAccount:"+email),
		},
	}
	ok, err := e.Evaluate(email, false, "cloudkms.cryptoKeys.create", resource)
	if err != nil || !ok {
		t.Fatalf("admin create: ok=%v err=%v", ok, err)
	}
	for _, perm := range []string{
		"cloudkms.cryptoKeyVersions.useToDecrypt",
		"cloudkms.cryptoKeyVersions.useToEncrypt",
	} {
		ok, err := e.Evaluate(email, false, perm, resource)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatalf("cloudkms.admin must not grant %s", perm)
		}
	}
}
