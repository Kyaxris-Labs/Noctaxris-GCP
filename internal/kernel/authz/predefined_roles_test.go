package authz_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
)

func evalRole(t *testing.T, role, email, perm, resource string) bool {
	t.Helper()
	e := &authz.Evaluator{
		Policies: memPolicies{
			resource: mustPolicy(t, role, "serviceAccount:"+email),
		},
	}
	ok, err := e.Evaluate(email, false, perm, resource)
	if err != nil {
		t.Fatalf("%s %s: %v", role, perm, err)
	}
	return ok
}

func TestPredefinedIAMRolesNarrowGrants(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "iam-reviewer@noctaxris-gcp-local.iam.gserviceaccount.com"

	if !evalRole(t, "roles/iam.securityReviewer", email, "iam.roles.get", resource) {
		t.Fatal("securityReviewer should get iam.roles.get")
	}
	if !evalRole(t, "roles/iam.securityReviewer", email, "iam.serviceAccounts.getIamPolicy", resource) {
		t.Fatal("securityReviewer should get getIamPolicy")
	}
	for _, perm := range []string{
		"iam.serviceAccounts.getAccessToken",
		"iam.roles.create",
		"iam.serviceAccounts.setIamPolicy",
	} {
		if evalRole(t, "roles/iam.securityReviewer", email, perm, resource) {
			t.Fatalf("securityReviewer must not grant %s", perm)
		}
	}

	if !evalRole(t, "roles/iam.roleViewer", email, "iam.roles.list", resource) {
		t.Fatal("roleViewer should list roles")
	}
	if evalRole(t, "roles/iam.roleViewer", email, "iam.serviceAccounts.setIamPolicy", resource) {
		t.Fatal("roleViewer must not setIamPolicy")
	}

	if !evalRole(t, "roles/iam.serviceAccountUser", email, "iam.serviceAccounts.actAs", resource) {
		t.Fatal("serviceAccountUser should grant actAs")
	}
	if evalRole(t, "roles/iam.serviceAccountUser", email, "iam.serviceAccounts.getAccessToken", resource) {
		t.Fatal("serviceAccountUser must not mint access tokens")
	}

	if !evalRole(t, "roles/resourcemanager.projectViewer", email, "resourcemanager.projects.get", resource) {
		t.Fatal("projectViewer should get project")
	}
	if evalRole(t, "roles/resourcemanager.projectViewer", email, "resourcemanager.projects.setIamPolicy", resource) {
		t.Fatal("projectViewer must not setIamPolicy")
	}
	if evalRole(t, "roles/resourcemanager.folderViewer", email, "resourcemanager.folders.setIamPolicy", "folders/team-a") {
		t.Fatal("folderViewer must not setIamPolicy")
	}
}

func TestPredefinedPubSubRolesNarrowGrants(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "pubsub@noctaxris-gcp-local.iam.gserviceaccount.com"

	if !evalRole(t, "roles/pubsub.viewer", email, "pubsub.topics.get", resource) {
		t.Fatal("pubsub.viewer should get topics")
	}
	for _, perm := range []string{"pubsub.topics.publish", "pubsub.subscriptions.consume", "pubsub.topics.delete"} {
		if evalRole(t, "roles/pubsub.viewer", email, perm, resource) {
			t.Fatalf("pubsub.viewer must not grant %s", perm)
		}
	}
	if !evalRole(t, "roles/pubsub.publisher", email, "pubsub.topics.publish", resource) {
		t.Fatal("publisher should publish")
	}
	if evalRole(t, "roles/pubsub.publisher", email, "pubsub.subscriptions.consume", resource) {
		t.Fatal("publisher must not consume")
	}
	if !evalRole(t, "roles/pubsub.subscriber", email, "pubsub.subscriptions.consume", resource) {
		t.Fatal("subscriber should consume")
	}
	if evalRole(t, "roles/pubsub.subscriber", email, "pubsub.topics.delete", resource) {
		t.Fatal("subscriber must not delete topics")
	}
	if !evalRole(t, "roles/pubsub.admin", email, "pubsub.topics.delete", resource) {
		t.Fatal("admin should delete topics")
	}
}

func TestPredefinedBigQueryRolesNarrowGrants(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "bq@noctaxris-gcp-local.iam.gserviceaccount.com"

	if !evalRole(t, "roles/bigquery.metadataViewer", email, "bigquery.tables.get", resource) {
		t.Fatal("metadataViewer should get tables")
	}
	if evalRole(t, "roles/bigquery.metadataViewer", email, "bigquery.tables.getData", resource) {
		t.Fatal("metadataViewer must not getData")
	}
	if !evalRole(t, "roles/bigquery.dataViewer", email, "bigquery.tables.getData", resource) {
		t.Fatal("dataViewer should getData")
	}
	if evalRole(t, "roles/bigquery.dataViewer", email, "bigquery.tables.updateData", resource) {
		t.Fatal("dataViewer must not updateData")
	}
	if !evalRole(t, "roles/bigquery.jobUser", email, "bigquery.jobs.create", resource) {
		t.Fatal("jobUser should create jobs")
	}
	if evalRole(t, "roles/bigquery.jobUser", email, "bigquery.datasets.delete", resource) {
		t.Fatal("jobUser must not delete datasets")
	}
	if !evalRole(t, "roles/bigquery.admin", email, "bigquery.datasets.delete", resource) {
		t.Fatal("admin should delete datasets")
	}
}

func TestPredefinedLoggingMonitoringRolesNarrowGrants(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "obs@noctaxris-gcp-local.iam.gserviceaccount.com"

	if !evalRole(t, "roles/logging.viewer", email, "logging.logEntries.list", resource) {
		t.Fatal("logging.viewer should list entries")
	}
	if evalRole(t, "roles/logging.viewer", email, "logging.sinks.create", resource) {
		t.Fatal("logging.viewer must not create sinks")
	}
	if !evalRole(t, "roles/logging.logWriter", email, "logging.logEntries.create", resource) {
		t.Fatal("logWriter should create entries")
	}
	if evalRole(t, "roles/logging.logWriter", email, "logging.sinks.delete", resource) {
		t.Fatal("logWriter must not delete sinks")
	}
	if !evalRole(t, "roles/monitoring.viewer", email, "monitoring.timeSeries.list", resource) {
		t.Fatal("monitoring.viewer should list time series")
	}
	if evalRole(t, "roles/monitoring.viewer", email, "monitoring.alertPolicies.create", resource) {
		t.Fatal("monitoring.viewer must not create alert policies")
	}
	if !evalRole(t, "roles/monitoring.admin", email, "monitoring.alertPolicies.create", resource) {
		t.Fatal("monitoring.admin should create alert policies")
	}
}

func TestPredefinedRunFunctionsRolesNarrowGrants(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "run@noctaxris-gcp-local.iam.gserviceaccount.com"

	if !evalRole(t, "roles/run.viewer", email, "run.services.get", resource) {
		t.Fatal("run.viewer should get services")
	}
	if evalRole(t, "roles/run.viewer", email, "run.services.create", resource) {
		t.Fatal("run.viewer must not create services")
	}
	if evalRole(t, "roles/run.viewer", email, "run.routes.invoke", resource) {
		t.Fatal("run.viewer must not invoke")
	}
	if !evalRole(t, "roles/run.developer", email, "run.services.update", resource) {
		t.Fatal("run.developer should update services")
	}
	if evalRole(t, "roles/run.developer", email, "run.services.setIamPolicy", resource) {
		t.Fatal("run.developer must not setIamPolicy")
	}
	if !evalRole(t, "roles/cloudfunctions.viewer", email, "cloudfunctions.functions.list", resource) {
		t.Fatal("cloudfunctions.viewer should list")
	}
	if evalRole(t, "roles/cloudfunctions.viewer", email, "cloudfunctions.functions.delete", resource) {
		t.Fatal("cloudfunctions.viewer must not delete")
	}
	if !evalRole(t, "roles/cloudfunctions.admin", email, "cloudfunctions.functions.delete", resource) {
		t.Fatal("cloudfunctions.admin should delete")
	}
}

func TestPredefinedServiceUsageRolesNarrowGrants(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "su@noctaxris-gcp-local.iam.gserviceaccount.com"

	if !evalRole(t, "roles/serviceusage.serviceUsageViewer", email, "serviceusage.services.get", resource) {
		t.Fatal("serviceUsageViewer should get")
	}
	for _, role := range []string{
		"roles/serviceusage.serviceUsageViewer",
		"roles/serviceusage.serviceUsageConsumer",
	} {
		for _, perm := range []string{"serviceusage.services.enable", "serviceusage.services.disable"} {
			if evalRole(t, role, email, perm, resource) {
				t.Fatalf("%s must not grant %s", role, perm)
			}
		}
	}
	if !evalRole(t, "roles/serviceusage.serviceUsageAdmin", email, "serviceusage.services.enable", resource) {
		t.Fatal("serviceUsageAdmin should enable")
	}
}

func TestPredefinedACMRolesNarrowGrants(t *testing.T) {
	resource := "organizations/noctaxris-gcp-org"
	email := "acm@noctaxris-gcp-local.iam.gserviceaccount.com"

	for _, role := range []string{
		"roles/accesscontextmanager.policyReader",
		"roles/accesscontextmanager.viewer",
	} {
		if !evalRole(t, role, email, "accesscontextmanager.servicePerimeters.get", resource) {
			t.Fatalf("%s should get perimeters", role)
		}
		for _, perm := range []string{
			"accesscontextmanager.servicePerimeters.update",
			"accesscontextmanager.policies.create",
			"accesscontextmanager.policies.delete",
		} {
			if evalRole(t, role, email, perm, resource) {
				t.Fatalf("%s must not grant %s", role, perm)
			}
		}
	}
	if !evalRole(t, "roles/accesscontextmanager.policyAdmin", email, "accesscontextmanager.policies.create", resource) {
		t.Fatal("policyAdmin should create policies")
	}
}

func TestEditorDeniesSecretKMSAndOrgPolicyMutate(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "editor@noctaxris-gcp-local.iam.gserviceaccount.com"

	if !evalRole(t, "roles/editor", email, "storage.buckets.create", resource) {
		t.Fatal("editor should still create buckets")
	}
	for _, perm := range []string{
		"secretmanager.versions.access",
		"cloudkms.cryptoKeyVersions.useToDecrypt",
		"cloudkms.cryptoKeyVersions.useToEncrypt",
		"cloudkms.cryptoKeyVersions.useToSign",
		"orgpolicy.policies.create",
		"orgpolicy.policies.update",
		"orgpolicy.policies.delete",
	} {
		if evalRole(t, "roles/editor", email, perm, resource) {
			t.Fatalf("editor must not grant %s", perm)
		}
	}
	if !evalRole(t, "roles/orgpolicy.policyAdmin", email, "orgpolicy.policies.update", resource) {
		t.Fatal("orgpolicy.policyAdmin should update policies")
	}
	if evalRole(t, "roles/orgpolicy.policyViewer", email, "orgpolicy.policies.update", resource) {
		t.Fatal("orgpolicy.policyViewer must not update")
	}
}

func TestPredefinedBinaryAssetTasksRolesNarrowGrants(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "misc@noctaxris-gcp-local.iam.gserviceaccount.com"

	if !evalRole(t, "roles/binaryauthorization.policyViewer", email, "binaryauthorization.policy.get", resource) {
		t.Fatal("binauthz viewer should get policy")
	}
	if evalRole(t, "roles/binaryauthorization.policyViewer", email, "binaryauthorization.policy.update", resource) {
		t.Fatal("binauthz viewer must not update")
	}
	if !evalRole(t, "roles/cloudasset.viewer", email, "cloudasset.assets.searchAllResources", resource) {
		t.Fatal("cloudasset.viewer should search")
	}
	if evalRole(t, "roles/cloudasset.viewer", email, "cloudasset.feeds.create", resource) {
		t.Fatal("cloudasset.viewer must not create feeds")
	}
	if !evalRole(t, "roles/containeranalysis.occurrences.viewer", email, "containeranalysis.occurrences.list", resource) {
		t.Fatal("occurrences.viewer should list")
	}
	if evalRole(t, "roles/containeranalysis.occurrences.viewer", email, "containeranalysis.occurrences.create", resource) {
		t.Fatal("occurrences.viewer must not create")
	}
	if !evalRole(t, "roles/cloudtasks.viewer", email, "cloudtasks.queues.list", resource) {
		t.Fatal("cloudtasks.viewer should list queues")
	}
	if evalRole(t, "roles/cloudtasks.viewer", email, "cloudtasks.tasks.create", resource) {
		t.Fatal("cloudtasks.viewer must not create tasks")
	}
	if !evalRole(t, "roles/cloudtasks.enqueuer", email, "cloudtasks.tasks.create", resource) {
		t.Fatal("enqueuer should create tasks")
	}
	if evalRole(t, "roles/cloudtasks.enqueuer", email, "cloudtasks.queues.delete", resource) {
		t.Fatal("enqueuer must not delete queues")
	}
}

func TestPredefinedStorageArtifactDataRolesNarrowGrants(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "data@noctaxris-gcp-local.iam.gserviceaccount.com"

	if !evalRole(t, "roles/storage.objectViewer", email, "storage.objects.get", resource) {
		t.Fatal("objectViewer should get objects")
	}
	for _, perm := range []string{"storage.objects.create", "storage.objects.delete", "storage.buckets.create"} {
		if evalRole(t, "roles/storage.objectViewer", email, perm, resource) {
			t.Fatalf("objectViewer must not grant %s", perm)
		}
	}
	if !evalRole(t, "roles/artifactregistry.reader", email, "artifactregistry.repositories.downloadArtifacts", resource) {
		t.Fatal("artifactregistry.reader should download")
	}
	if evalRole(t, "roles/artifactregistry.reader", email, "artifactregistry.repositories.uploadArtifacts", resource) {
		t.Fatal("reader must not upload")
	}
	if !evalRole(t, "roles/datastore.viewer", email, "datastore.entities.get", resource) {
		t.Fatal("datastore.viewer should get entities")
	}
	if evalRole(t, "roles/datastore.viewer", email, "datastore.entities.create", resource) {
		t.Fatal("datastore.viewer must not create")
	}
	if !evalRole(t, "roles/firestore.user", email, "datastore.entities.update", resource) {
		t.Fatal("firestore.user should update entities")
	}
	if evalRole(t, "roles/firestore.viewer", email, "datastore.entities.delete", resource) {
		t.Fatal("firestore.viewer must not delete")
	}
	if !evalRole(t, "roles/spanner.databaseUser", email, "spanner.databases.write", resource) {
		t.Fatal("spanner.databaseUser should write")
	}
	if evalRole(t, "roles/spanner.viewer", email, "spanner.databases.write", resource) {
		t.Fatal("spanner.viewer must not write")
	}
	if !evalRole(t, "roles/cloudscheduler.jobRunner", email, "cloudscheduler.jobs.run", resource) {
		t.Fatal("jobRunner should run")
	}
	if evalRole(t, "roles/cloudscheduler.viewer", email, "cloudscheduler.jobs.create", resource) {
		t.Fatal("scheduler viewer must not create")
	}
	if !evalRole(t, "roles/eventarc.viewer", email, "eventarc.triggers.list", resource) {
		t.Fatal("eventarc.viewer should list")
	}
	if evalRole(t, "roles/eventarc.viewer", email, "eventarc.triggers.create", resource) {
		t.Fatal("eventarc.viewer must not create")
	}
	if !evalRole(t, "roles/cloudbuild.builds.viewer", email, "cloudbuild.builds.get", resource) {
		t.Fatal("cloudbuild viewer should get builds")
	}
	if evalRole(t, "roles/cloudbuild.builds.viewer", email, "cloudbuild.builds.create", resource) {
		t.Fatal("cloudbuild viewer must not create")
	}
	if !evalRole(t, "roles/firebaseauth.viewer", email, "firebaseauth.users.get", resource) {
		t.Fatal("firebaseauth.viewer should get users")
	}
	if evalRole(t, "roles/firebaseauth.viewer", email, "firebaseauth.users.create", resource) {
		t.Fatal("firebaseauth.viewer must not create")
	}
	if !evalRole(t, "roles/identitytoolkit.viewer", email, "identitytoolkit.tenants.list", resource) {
		t.Fatal("identitytoolkit.viewer should list tenants")
	}
	if evalRole(t, "roles/identitytoolkit.viewer", email, "identitytoolkit.tenants.create", resource) {
		t.Fatal("identitytoolkit.viewer must not create")
	}
	// Residual prefix path must stay empty: unknown roles/xyz.* fail closed.
	if evalRole(t, "roles/xyz.admin", email, "xyz.things.create", resource) {
		t.Fatal("unknown service prefix must fail closed")
	}
}
