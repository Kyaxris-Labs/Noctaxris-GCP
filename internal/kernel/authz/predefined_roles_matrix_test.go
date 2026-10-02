package authz_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
)

// Table-driven smoke of marketed predefined roles: one positive grant and one
// negative (unrelated mutate) per role to exercise grant helpers.
func TestPredefinedRolesAllowAndDenyMatrix(t *testing.T) {
	resource := "projects/noctaxris-gcp-local"
	email := "role-matrix@noctaxris-gcp-local.iam.gserviceaccount.com"
	cases := []struct {
		role   string
		allow  string
		deny   string
	}{
		{"roles/iam.roleAdmin", "iam.roles.create", "storage.buckets.delete"},
		{"roles/iam.serviceAccountViewer", "iam.serviceAccounts.get", "iam.serviceAccounts.actAs"},
		{"roles/iam.workloadIdentityPoolAdmin", "iam.workloadIdentityPools.create", "storage.objects.delete"},
		{"roles/iam.workloadIdentityPoolViewer", "iam.workloadIdentityPools.get", "iam.workloadIdentityPools.delete"},
		{"roles/resourcemanager.organizationAdmin", "resourcemanager.organizations.get", "secretmanager.versions.access"},
		{"roles/resourcemanager.organizationViewer", "resourcemanager.organizations.get", "resourcemanager.projects.create"},
		{"roles/resourcemanager.folderAdmin", "resourcemanager.folders.create", "iam.serviceAccounts.actAs"},
		{"roles/resourcemanager.folderEditor", "resourcemanager.folders.update", "resourcemanager.folders.setIamPolicy"},
		{"roles/resourcemanager.folderIamAdmin", "resourcemanager.folders.setIamPolicy", "resourcemanager.folders.create"},
		{"roles/resourcemanager.projectIamAdmin", "resourcemanager.projects.setIamPolicy", "resourcemanager.projects.create"},
		{"roles/resourcemanager.projectCreator", "resourcemanager.projects.create", "resourcemanager.projects.delete"},
		{"roles/resourcemanager.projectMover", "resourcemanager.projects.update", "resourcemanager.projects.delete"},
		{"roles/resourcemanager.tagAdmin", "resourcemanager.tagKeys.create", "storage.buckets.delete"},
		{"roles/resourcemanager.tagUser", "resourcemanager.tagBindings.create", "resourcemanager.tagKeys.create"},
		{"roles/resourcemanager.tagViewer", "resourcemanager.tagKeys.get", "resourcemanager.tagBindings.create"},
		{"roles/pubsub.admin", "pubsub.topics.create", "iam.serviceAccounts.actAs"},
		{"roles/pubsub.editor", "pubsub.topics.publish", "iam.roles.create"},
		{"roles/bigquery.admin", "bigquery.datasets.create", "iam.serviceAccounts.actAs"},
		{"roles/bigquery.dataOwner", "bigquery.tables.updateData", "bigquery.jobs.create"},
		{"roles/bigquery.dataEditor", "bigquery.tables.updateData", "iam.serviceAccounts.actAs"},
		{"roles/bigquery.metadataViewer", "bigquery.tables.get", "bigquery.tables.updateData"},
		{"roles/bigquery.user", "bigquery.jobs.create", "bigquery.datasets.delete"},
		{"roles/bigquery.jobUser", "bigquery.jobs.create", "bigquery.jobs.get"},
		{"roles/accesscontextmanager.policyEditor", "accesscontextmanager.policies.update", "accesscontextmanager.policies.create"},
		{"roles/cloudkms.signerVerifier", "cloudkms.cryptoKeyVersions.useToSign", "cloudkms.cryptoKeyVersions.useToDecrypt"},
		{"roles/cloudkms.publicKeyViewer", "cloudkms.cryptoKeyVersions.viewPublicKey", "cloudkms.cryptoKeyVersions.useToSign"},
		{"roles/logging.admin", "logging.logs.delete", "iam.serviceAccounts.actAs"},
		{"roles/logging.logWriter", "logging.logEntries.create", "logging.logs.delete"},
		{"roles/logging.configWriter", "logging.sinks.create", "iam.serviceAccounts.actAs"},
		{"roles/monitoring.admin", "monitoring.alertPolicies.create", "iam.roles.create"},
		{"roles/monitoring.metricWriter", "monitoring.timeSeries.create", "monitoring.alertPolicies.delete"},
		{"roles/monitoring.alertPolicyEditor", "monitoring.alertPolicies.create", "monitoring.dashboards.delete"},
		{"roles/run.admin", "run.services.create", "iam.serviceAccounts.actAs"},
		{"roles/run.developer", "run.services.update", "run.services.setIamPolicy"},
		{"roles/run.viewer", "run.services.get", "run.services.create"},
		{"roles/cloudfunctions.admin", "cloudfunctions.functions.create", "iam.roles.create"},
		{"roles/cloudfunctions.developer", "cloudfunctions.functions.update", "cloudfunctions.functions.setIamPolicy"},
		{"roles/cloudfunctions.viewer", "cloudfunctions.functions.get", "cloudfunctions.functions.delete"},
		{"roles/storage.admin", "storage.buckets.create", "iam.serviceAccounts.actAs"},
		{"roles/storage.objectAdmin", "storage.objects.delete", "storage.buckets.delete"},
		{"roles/storage.objectCreator", "storage.objects.create", "storage.objects.delete"},
		{"roles/storage.hmacKeyAdmin", "storage.hmacKeys.create", "storage.buckets.delete"},
		{"roles/storage.legacyBucketReader", "storage.buckets.get", "storage.buckets.delete"},
		{"roles/storage.legacyObjectReader", "storage.objects.get", "storage.objects.delete"},
		{"roles/artifactregistry.admin", "artifactregistry.repositories.create", "iam.roles.create"},
		{"roles/artifactregistry.repoAdmin", "artifactregistry.repositories.update", "iam.serviceAccounts.actAs"},
		{"roles/artifactregistry.writer", "artifactregistry.packages.create", "artifactregistry.repositories.delete"},
		{"roles/artifactregistry.reader", "artifactregistry.packages.get", "artifactregistry.repositories.create"},
		{"roles/datastore.owner", "datastore.entities.create", "iam.serviceAccounts.actAs"},
		{"roles/firestore.owner", "datastore.entities.create", "iam.roles.create"},
		{"roles/datastore.user", "datastore.entities.create", "datastore.databases.delete"},
		{"roles/firestore.user", "datastore.entities.create", "datastore.databases.delete"},
		{"roles/datastore.viewer", "datastore.entities.get", "datastore.entities.create"},
		{"roles/firestore.viewer", "datastore.entities.get", "datastore.entities.create"},
		{"roles/spanner.admin", "spanner.instances.create", "iam.roles.create"},
		{"roles/spanner.databaseAdmin", "spanner.databases.create", "iam.serviceAccounts.actAs"},
		{"roles/spanner.databaseUser", "spanner.databases.write", "spanner.databases.drop"},
		{"roles/spanner.viewer", "spanner.instances.get", "spanner.instances.create"},
		{"roles/spanner.databaseReader", "spanner.databases.read", "spanner.databases.write"},
		{"roles/cloudscheduler.admin", "cloudscheduler.jobs.create", "iam.roles.create"},
		{"roles/cloudscheduler.jobRunner", "cloudscheduler.jobs.run", "cloudscheduler.jobs.delete"},
		{"roles/cloudscheduler.viewer", "cloudscheduler.jobs.get", "cloudscheduler.jobs.create"},
		{"roles/eventarc.admin", "eventarc.triggers.create", "iam.roles.create"},
		{"roles/eventarc.developer", "eventarc.triggers.update", "eventarc.triggers.setIamPolicy"},
		{"roles/eventarc.eventReceiver", "eventarc.events.receiveEvent", "eventarc.triggers.delete"},
		{"roles/eventarc.viewer", "eventarc.triggers.get", "eventarc.triggers.create"},
		{"roles/cloudbuild.builds.editor", "cloudbuild.builds.create", "iam.roles.create"},
		{"roles/cloudbuild.builds.viewer", "cloudbuild.builds.get", "cloudbuild.builds.create"},
		{"roles/firebaseauth.admin", "firebaseauth.configs.update", "iam.roles.create"},
		{"roles/firebaseauth.viewer", "firebaseauth.users.get", "firebaseauth.configs.update"},
		{"roles/identitytoolkit.admin", "identitytoolkit.tenants.create", "iam.roles.create"},
		{"roles/identitytoolkit.viewer", "identitytoolkit.tenants.get", "identitytoolkit.tenants.delete"},
		{"roles/secretmanager.admin", "secretmanager.secrets.create", "iam.serviceAccounts.actAs"},
		{"roles/cloudkms.admin", "cloudkms.cryptoKeys.create", "iam.serviceAccounts.actAs"},
		{"roles/cloudkms.cryptoKeyEncrypter", "cloudkms.cryptoKeyVersions.useToEncrypt", "cloudkms.cryptoKeyVersions.useToDecrypt"},
		{"roles/cloudkms.cryptoKeyDecrypter", "cloudkms.cryptoKeyVersions.useToDecrypt", "cloudkms.cryptoKeyVersions.useToEncrypt"},
		{"roles/serviceusage.serviceUsageAdmin", "serviceusage.services.enable", "iam.roles.create"},
		{"roles/cloudbuild.workerPoolUser", "cloudbuild.workerpools.use", "cloudbuild.workerpools.delete"},
		{"roles/logging.viewAccessor", "logging.views.get", "logging.logs.delete"},
		{"roles/iam.securityAdmin", "iam.roles.create", "storage.buckets.delete"},
		{"roles/iam.serviceAccountAdmin", "iam.serviceAccounts.create", "iam.serviceAccounts.getAccessToken"},
		{"roles/orgpolicy.policyAdmin", "orgpolicy.policies.create", "iam.serviceAccounts.actAs"},
		{"roles/orgpolicy.policyViewer", "orgpolicy.policies.get", "orgpolicy.policies.create"},
		{"roles/accesscontextmanager.policyAdmin", "accesscontextmanager.policies.create", "iam.roles.create"},
		{"roles/accesscontextmanager.policyReader", "accesscontextmanager.policies.get", "accesscontextmanager.policies.delete"},
		{"roles/binaryauthorization.policyAdmin", "binaryauthorization.policy.update", "iam.roles.create"},
		{"roles/binaryauthorization.policyViewer", "binaryauthorization.policy.get", "binaryauthorization.policy.update"},
		{"roles/cloudasset.owner", "cloudasset.assets.exportResource", "iam.roles.create"},
		{"roles/cloudasset.viewer", "cloudasset.assets.listResource", "cloudasset.assets.exportResource"},
		{"roles/containeranalysis.admin", "containeranalysis.notes.create", "iam.roles.create"},
		{"roles/containeranalysis.occurrences.viewer", "containeranalysis.occurrences.get", "containeranalysis.notes.create"},
		{"roles/cloudtasks.admin", "cloudtasks.queues.create", "iam.roles.create"},
		{"roles/cloudtasks.viewer", "cloudtasks.queues.get", "cloudtasks.queues.create"},
		{"roles/cloudtasks.enqueuer", "cloudtasks.tasks.create", "cloudtasks.queues.delete"},
		{"roles/cloudtasks.taskRunner", "cloudtasks.tasks.run", "cloudtasks.queues.create"},
		{"roles/cloudtasks.queueAdmin", "cloudtasks.queues.update", "iam.roles.create"},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			if !evalRole(t, tc.role, email, tc.allow, resource) {
				t.Fatalf("%s should allow %s", tc.role, tc.allow)
			}
			if evalRole(t, tc.role, email, tc.deny, resource) {
				t.Fatalf("%s must deny %s", tc.role, tc.deny)
			}
		})
	}

	e := &authz.Evaluator{Policies: memPolicies{
		resource: mustPolicy(t, "roles/completely.unknown.role", "serviceAccount:"+email),
	}}
	ok, err := e.Evaluate(email, false, "storage.buckets.get", resource)
	if err != nil || ok {
		t.Fatalf("unknown predefined role must fail closed: ok=%v err=%v", ok, err)
	}
}
