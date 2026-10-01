package authz

import "strings"

// predefinedServiceRoleGrants maps marketed predefined roles to documented
// permission sets. known is true when the role name is handled here (allow or
// deny); false means the caller should try other grant paths.
func predefinedServiceRoleGrants(role, permission string) (granted bool, known bool) {
	switch role {
	// IAM
	case "roles/iam.securityReviewer":
		return iamSecurityReviewerGrants(permission), true
	case "roles/iam.roleViewer":
		return permission == "iam.roles.get" || permission == "iam.roles.list", true
	case "roles/iam.roleAdmin":
		return strings.HasPrefix(permission, "iam.roles."), true
	case "roles/iam.serviceAccountUser":
		return permission == "iam.serviceAccounts.actAs", true
	case "roles/iam.serviceAccountViewer":
		return iamServiceAccountViewerGrants(permission), true
	case "roles/iam.workloadIdentityPoolAdmin":
		return strings.HasPrefix(permission, "iam.workloadIdentityPools.") ||
			strings.HasPrefix(permission, "iam.workloadIdentityPoolProviders."), true
	case "roles/iam.workloadIdentityPoolViewer":
		return iamWorkloadIdentityViewerGrants(permission), true

	// Resource Manager
	case "roles/resourcemanager.organizationAdmin":
		return strings.HasPrefix(permission, "resourcemanager.organizations.") ||
			strings.HasPrefix(permission, "resourcemanager.folders.") ||
			strings.HasPrefix(permission, "resourcemanager.projects."), true
	case "roles/resourcemanager.organizationViewer":
		return permission == "resourcemanager.organizations.get" ||
			permission == "resourcemanager.organizations.getIamPolicy", true
	case "roles/resourcemanager.folderAdmin":
		return strings.HasPrefix(permission, "resourcemanager.folders."), true
	case "roles/resourcemanager.folderEditor":
		return resourcemanagerFolderEditorGrants(permission), true
	case "roles/resourcemanager.folderViewer":
		return resourcemanagerFolderViewerGrants(permission), true
	case "roles/resourcemanager.folderIamAdmin":
		return permission == "resourcemanager.folders.getIamPolicy" ||
			permission == "resourcemanager.folders.setIamPolicy" ||
			permission == "resourcemanager.folders.get", true
	case "roles/resourcemanager.projectIamAdmin":
		return permission == "resourcemanager.projects.getIamPolicy" ||
			permission == "resourcemanager.projects.setIamPolicy" ||
			permission == "resourcemanager.projects.get", true
	case "roles/resourcemanager.projectViewer":
		return resourcemanagerProjectViewerGrants(permission), true
	case "roles/resourcemanager.projectCreator":
		return permission == "resourcemanager.projects.create", true
	case "roles/resourcemanager.projectMover":
		return permission == "resourcemanager.projects.update" ||
			permission == "resourcemanager.projects.get", true
	case "roles/resourcemanager.tagAdmin":
		return strings.HasPrefix(permission, "resourcemanager.tagKeys.") ||
			strings.HasPrefix(permission, "resourcemanager.tagBindings."), true
	case "roles/resourcemanager.tagUser":
		return permission == "resourcemanager.tagBindings.create" ||
			permission == "resourcemanager.tagBindings.delete" ||
			permission == "resourcemanager.tagBindings.get" ||
			permission == "resourcemanager.tagBindings.list" ||
			permission == "resourcemanager.tagKeys.get" ||
			permission == "resourcemanager.tagKeys.list", true
	case "roles/resourcemanager.tagViewer":
		return permission == "resourcemanager.tagBindings.get" ||
			permission == "resourcemanager.tagBindings.list" ||
			permission == "resourcemanager.tagKeys.get" ||
			permission == "resourcemanager.tagKeys.list", true

	// Service Usage
	case "roles/serviceusage.serviceUsageConsumer":
		return serviceusageConsumerGrants(permission), true
	case "roles/serviceusage.serviceUsageViewer":
		return permission == "serviceusage.services.get" ||
			permission == "serviceusage.services.list", true

	// Pub/Sub
	case "roles/pubsub.admin", "roles/pubsub.editor":
		return strings.HasPrefix(permission, "pubsub."), true
	case "roles/pubsub.viewer":
		return pubsubViewerGrants(permission), true
	case "roles/pubsub.publisher":
		return pubsubPublisherGrants(permission), true
	case "roles/pubsub.subscriber":
		return pubsubSubscriberGrants(permission), true

	// BigQuery
	case "roles/bigquery.admin":
		return strings.HasPrefix(permission, "bigquery."), true
	case "roles/bigquery.dataOwner", "roles/bigquery.dataEditor":
		return bigqueryDataEditorGrants(permission), true
	case "roles/bigquery.dataViewer":
		return bigqueryDataViewerGrants(permission), true
	case "roles/bigquery.metadataViewer":
		return bigqueryMetadataViewerGrants(permission), true
	case "roles/bigquery.user":
		return bigqueryUserGrants(permission), true
	case "roles/bigquery.jobUser":
		return permission == "bigquery.jobs.create" || permission == "bigquery.jobs.get", true

	// Logging
	case "roles/logging.admin":
		return strings.HasPrefix(permission, "logging."), true
	case "roles/logging.viewer", "roles/logging.privateLogViewer":
		return loggingViewerGrants(permission), true
	case "roles/logging.logWriter":
		return permission == "logging.logEntries.create", true
	case "roles/logging.configWriter":
		return loggingConfigWriterGrants(permission), true

	// Monitoring
	case "roles/monitoring.admin":
		return strings.HasPrefix(permission, "monitoring."), true
	case "roles/monitoring.viewer":
		return monitoringViewerGrants(permission), true
	case "roles/monitoring.metricWriter":
		return permission == "monitoring.timeSeries.create" ||
			permission == "monitoring.metricDescriptors.create", true
	case "roles/monitoring.alertPolicyEditor":
		return strings.HasPrefix(permission, "monitoring.alertPolicies."), true

	// Cloud Run
	case "roles/run.admin":
		return strings.HasPrefix(permission, "run."), true
	case "roles/run.developer":
		return runDeveloperGrants(permission), true
	case "roles/run.viewer":
		return runViewerGrants(permission), true

	// Cloud Functions
	case "roles/cloudfunctions.admin":
		return strings.HasPrefix(permission, "cloudfunctions."), true
	case "roles/cloudfunctions.developer":
		return cloudfunctionsDeveloperGrants(permission), true
	case "roles/cloudfunctions.viewer":
		return cloudfunctionsViewerGrants(permission), true

	// Access Context Manager
	case "roles/accesscontextmanager.policyAdmin",
		"roles/accesscontextmanager.gcpAccessAdmin",
		"roles/accesscontextmanager.admin":
		return strings.HasPrefix(permission, "accesscontextmanager."), true
	case "roles/accesscontextmanager.policyEditor",
		"roles/accesscontextmanager.editor":
		return accesscontextmanagerEditorGrants(permission), true
	case "roles/accesscontextmanager.policyReader",
		"roles/accesscontextmanager.viewer":
		return accesscontextmanagerViewerGrants(permission), true

	// Binary Authorization
	case "roles/binaryauthorization.policyAdmin",
		"roles/binaryauthorization.attestorsAdmin",
		"roles/binaryauthorization.admin":
		return strings.HasPrefix(permission, "binaryauthorization."), true
	case "roles/binaryauthorization.policyEditor",
		"roles/binaryauthorization.editor":
		return permission == "binaryauthorization.policy.get" ||
			permission == "binaryauthorization.policy.update", true
	case "roles/binaryauthorization.policyViewer",
		"roles/binaryauthorization.attestorsViewer",
		"roles/binaryauthorization.viewer":
		return permission == "binaryauthorization.policy.get", true

	// Cloud Asset Inventory
	case "roles/cloudasset.owner", "roles/cloudasset.admin":
		return strings.HasPrefix(permission, "cloudasset."), true
	case "roles/cloudasset.viewer":
		return cloudassetViewerGrants(permission), true

	// Container Analysis
	case "roles/containeranalysis.admin",
		"roles/containeranalysis.occurrences.editor",
		"roles/containeranalysis.notes.editor":
		return strings.HasPrefix(permission, "containeranalysis."), true
	case "roles/containeranalysis.occurrences.viewer",
		"roles/containeranalysis.notes.viewer",
		"roles/containeranalysis.viewer":
		return permission == "containeranalysis.occurrences.get" ||
			permission == "containeranalysis.occurrences.list", true

	// Cloud Tasks
	case "roles/cloudtasks.admin":
		return strings.HasPrefix(permission, "cloudtasks."), true
	case "roles/cloudtasks.viewer":
		return cloudtasksViewerGrants(permission), true
	case "roles/cloudtasks.enqueuer":
		return permission == "cloudtasks.tasks.create" ||
			permission == "cloudtasks.queues.get" ||
			permission == "cloudtasks.tasks.get" ||
			permission == "cloudtasks.tasks.list", true
	case "roles/cloudtasks.taskRunner":
		return permission == "cloudtasks.tasks.run" ||
			permission == "cloudtasks.tasks.get" ||
			permission == "cloudtasks.tasks.list", true
	case "roles/cloudtasks.queueAdmin", "roles/cloudtasks.editor":
		return cloudtasksEditorGrants(permission), true

	// Organization Policy
	case "roles/orgpolicy.policyAdmin":
		return strings.HasPrefix(permission, "orgpolicy."), true
	case "roles/orgpolicy.policyViewer":
		return permission == "orgpolicy.policies.get" ||
			permission == "orgpolicy.policies.list" ||
			permission == "orgpolicy.constraints.list", true

	// Cloud Storage
	case "roles/storage.admin":
		return strings.HasPrefix(permission, "storage."), true
	case "roles/storage.objectAdmin":
		return storageObjectAdminGrants(permission), true
	case "roles/storage.objectViewer":
		return storageObjectViewerGrants(permission), true
	case "roles/storage.objectCreator":
		return storageObjectCreatorGrants(permission), true
	case "roles/storage.hmacKeyAdmin":
		return strings.HasPrefix(permission, "storage.hmacKeys."), true
	case "roles/storage.legacyBucketReader":
		return permission == "storage.buckets.get" || permission == "storage.objects.list", true
	case "roles/storage.legacyObjectReader":
		return permission == "storage.objects.get", true

	// Artifact Registry
	case "roles/artifactregistry.admin":
		return strings.HasPrefix(permission, "artifactregistry."), true
	case "roles/artifactregistry.repoAdmin":
		return artifactregistryRepoAdminGrants(permission), true
	case "roles/artifactregistry.writer":
		return artifactregistryWriterGrants(permission), true
	case "roles/artifactregistry.reader":
		return artifactregistryReaderGrants(permission), true

	// Datastore / Firestore (lab Firestore checks datastore.entities.*)
	case "roles/datastore.owner", "roles/firestore.owner":
		return datastoreOwnerGrants(permission), true
	case "roles/datastore.user", "roles/firestore.user":
		return datastoreUserGrants(permission), true
	case "roles/datastore.viewer", "roles/firestore.viewer":
		return datastoreViewerGrants(permission), true

	// Cloud Spanner
	case "roles/spanner.admin":
		return strings.HasPrefix(permission, "spanner."), true
	case "roles/spanner.databaseAdmin":
		return spannerDatabaseAdminGrants(permission), true
	case "roles/spanner.databaseUser":
		return spannerDatabaseUserGrants(permission), true
	case "roles/spanner.viewer", "roles/spanner.databaseReader":
		return spannerViewerGrants(permission), true

	// Cloud Scheduler
	case "roles/cloudscheduler.admin":
		return strings.HasPrefix(permission, "cloudscheduler."), true
	case "roles/cloudscheduler.jobRunner":
		return cloudschedulerJobRunnerGrants(permission), true
	case "roles/cloudscheduler.viewer":
		return cloudschedulerViewerGrants(permission), true

	// Eventarc
	case "roles/eventarc.admin":
		return strings.HasPrefix(permission, "eventarc."), true
	case "roles/eventarc.developer":
		return eventarcDeveloperGrants(permission), true
	case "roles/eventarc.eventReceiver":
		return permission == "eventarc.events.receiveEvent" ||
			permission == "eventarc.triggers.get" ||
			permission == "eventarc.triggers.list", true
	case "roles/eventarc.viewer":
		return eventarcViewerGrants(permission), true

	// Cloud Build
	case "roles/cloudbuild.builds.builder", "roles/cloudbuild.builds.editor":
		return cloudbuildEditorGrants(permission), true
	case "roles/cloudbuild.builds.viewer":
		return cloudbuildViewerGrants(permission), true

	// Firebase Auth / Identity Toolkit
	case "roles/firebaseauth.admin":
		return strings.HasPrefix(permission, "firebaseauth."), true
	case "roles/firebaseauth.viewer":
		return firebaseauthViewerGrants(permission), true
	case "roles/identitytoolkit.admin":
		return strings.HasPrefix(permission, "identitytoolkit."), true
	case "roles/identitytoolkit.viewer":
		return identitytoolkitViewerGrants(permission), true

	default:
		return false, false
	}
}

func iamSecurityReviewerGrants(permission string) bool {
	if strings.HasSuffix(permission, ".getIamPolicy") {
		return true
	}
	switch permission {
	case "iam.roles.get", "iam.roles.list",
		"iam.serviceAccounts.get", "iam.serviceAccounts.list",
		"iam.serviceAccountKeys.get", "iam.serviceAccountKeys.list",
		"iam.workloadIdentityPools.get", "iam.workloadIdentityPools.list",
		"iam.workloadIdentityPoolProviders.get", "iam.workloadIdentityPoolProviders.list",
		"resourcemanager.projects.get", "resourcemanager.projects.getIamPolicy",
		"resourcemanager.folders.get", "resourcemanager.folders.getIamPolicy",
		"resourcemanager.organizations.get", "resourcemanager.organizations.getIamPolicy":
		return true
	default:
		return false
	}
}

func iamServiceAccountViewerGrants(permission string) bool {
	switch permission {
	case "iam.serviceAccounts.get", "iam.serviceAccounts.list",
		"iam.serviceAccountKeys.get", "iam.serviceAccountKeys.list",
		"iam.serviceAccounts.getIamPolicy":
		return true
	default:
		return false
	}
}

func iamWorkloadIdentityViewerGrants(permission string) bool {
	switch permission {
	case "iam.workloadIdentityPools.get", "iam.workloadIdentityPools.list",
		"iam.workloadIdentityPoolProviders.get", "iam.workloadIdentityPoolProviders.list":
		return true
	default:
		return false
	}
}

func resourcemanagerProjectViewerGrants(permission string) bool {
	switch permission {
	case "resourcemanager.projects.get",
		"resourcemanager.projects.getIamPolicy",
		"resourcemanager.projects.list",
		"resourcemanager.projects.search":
		return true
	default:
		return false
	}
}

func resourcemanagerFolderViewerGrants(permission string) bool {
	switch permission {
	case "resourcemanager.folders.get",
		"resourcemanager.folders.getIamPolicy",
		"resourcemanager.folders.list":
		return true
	default:
		return false
	}
}

func resourcemanagerFolderEditorGrants(permission string) bool {
	if resourcemanagerFolderViewerGrants(permission) {
		return true
	}
	switch permission {
	case "resourcemanager.folders.create",
		"resourcemanager.folders.update",
		"resourcemanager.folders.move",
		"resourcemanager.folders.delete",
		"resourcemanager.folders.undelete":
		return true
	default:
		return false
	}
}

func serviceusageConsumerGrants(permission string) bool {
	switch permission {
	case "serviceusage.services.get",
		"serviceusage.services.list":
		return true
	default:
		return false
	}
}

func pubsubViewerGrants(permission string) bool {
	switch permission {
	case "pubsub.topics.get", "pubsub.topics.list",
		"pubsub.subscriptions.get", "pubsub.subscriptions.list",
		"pubsub.snapshots.get", "pubsub.snapshots.list":
		return true
	default:
		return false
	}
}

func pubsubPublisherGrants(permission string) bool {
	switch permission {
	case "pubsub.topics.publish",
		"pubsub.topics.get", "pubsub.topics.list":
		return true
	default:
		return false
	}
}

func pubsubSubscriberGrants(permission string) bool {
	switch permission {
	case "pubsub.subscriptions.consume",
		"pubsub.subscriptions.get", "pubsub.subscriptions.list",
		"pubsub.snapshots.get", "pubsub.snapshots.list",
		"pubsub.topics.get", "pubsub.topics.list":
		return true
	default:
		return false
	}
}

func bigqueryMetadataViewerGrants(permission string) bool {
	switch permission {
	case "bigquery.datasets.get", "bigquery.datasets.list",
		"bigquery.tables.get", "bigquery.tables.list",
		"bigquery.jobs.get":
		return true
	default:
		return false
	}
}

func bigqueryDataViewerGrants(permission string) bool {
	if bigqueryMetadataViewerGrants(permission) {
		return true
	}
	return permission == "bigquery.tables.getData"
}

func bigqueryDataEditorGrants(permission string) bool {
	if bigqueryDataViewerGrants(permission) {
		return true
	}
	switch permission {
	case "bigquery.datasets.create", "bigquery.datasets.delete",
		"bigquery.tables.create", "bigquery.tables.delete",
		"bigquery.tables.updateData":
		return true
	default:
		return false
	}
}

func bigqueryUserGrants(permission string) bool {
	switch permission {
	case "bigquery.jobs.create", "bigquery.jobs.get",
		"bigquery.datasets.get", "bigquery.datasets.list",
		"bigquery.tables.get", "bigquery.tables.list",
		"bigquery.datasets.create":
		return true
	default:
		return false
	}
}

func loggingViewerGrants(permission string) bool {
	switch permission {
	case "logging.logEntries.list", "logging.logs.list",
		"logging.sinks.get", "logging.sinks.list",
		"logging.buckets.get", "logging.buckets.list",
		"logging.views.get", "logging.views.list",
		"logging.exclusions.get", "logging.exclusions.list":
		return true
	default:
		return false
	}
}

func loggingConfigWriterGrants(permission string) bool {
	if loggingViewerGrants(permission) {
		return true
	}
	switch permission {
	case "logging.sinks.create", "logging.sinks.update", "logging.sinks.delete",
		"logging.views.create",
		"logging.exclusions.create", "logging.exclusions.delete",
		"logging.logs.delete", "logging.entries.copy":
		return true
	default:
		return false
	}
}

func monitoringViewerGrants(permission string) bool {
	switch permission {
	case "monitoring.metricDescriptors.get", "monitoring.metricDescriptors.list",
		"monitoring.timeSeries.list",
		"monitoring.alertPolicies.get", "monitoring.alertPolicies.list":
		return true
	default:
		return false
	}
}

func runViewerGrants(permission string) bool {
	switch permission {
	case "run.services.get", "run.services.list", "run.services.getIamPolicy",
		"run.revisions.get", "run.revisions.list",
		"run.jobs.get", "run.jobs.list":
		return true
	default:
		return false
	}
}

func runDeveloperGrants(permission string) bool {
	if runViewerGrants(permission) {
		return true
	}
	switch permission {
	case "run.services.create", "run.services.update", "run.services.delete",
		"run.revisions.delete",
		"run.jobs.create", "run.jobs.update", "run.jobs.delete":
		return true
	default:
		return false
	}
}

func cloudfunctionsViewerGrants(permission string) bool {
	switch permission {
	case "cloudfunctions.functions.get", "cloudfunctions.functions.list",
		"cloudfunctions.functions.getIamPolicy",
		"cloudfunctions.functions.sourceCodeGet":
		return true
	default:
		return false
	}
}

func cloudfunctionsDeveloperGrants(permission string) bool {
	if cloudfunctionsViewerGrants(permission) {
		return true
	}
	switch permission {
	case "cloudfunctions.functions.create",
		"cloudfunctions.functions.update",
		"cloudfunctions.functions.delete":
		return true
	default:
		return false
	}
}

func accesscontextmanagerViewerGrants(permission string) bool {
	switch permission {
	case "accesscontextmanager.policies.get", "accesscontextmanager.policies.list",
		"accesscontextmanager.servicePerimeters.get", "accesscontextmanager.servicePerimeters.list":
		return true
	default:
		return false
	}
}

func accesscontextmanagerEditorGrants(permission string) bool {
	return strings.HasPrefix(permission, "accesscontextmanager.")
}

func cloudassetViewerGrants(permission string) bool {
	switch permission {
	case "cloudasset.assets.searchAllResources",
		"cloudasset.assets.listResource",
		"cloudasset.feeds.get", "cloudasset.feeds.list":
		return true
	default:
		return false
	}
}

func cloudtasksViewerGrants(permission string) bool {
	switch permission {
	case "cloudtasks.queues.get", "cloudtasks.queues.list",
		"cloudtasks.tasks.get", "cloudtasks.tasks.list":
		return true
	default:
		return false
	}
}

func cloudtasksEditorGrants(permission string) bool {
	if cloudtasksViewerGrants(permission) {
		return true
	}
	switch permission {
	case "cloudtasks.queues.create", "cloudtasks.queues.update", "cloudtasks.queues.delete",
		"cloudtasks.tasks.create", "cloudtasks.tasks.delete", "cloudtasks.tasks.run":
		return true
	default:
		return false
	}
}

func storageObjectViewerGrants(permission string) bool {
	switch permission {
	case "storage.objects.get", "storage.objects.list",
		"storage.buckets.get":
		return true
	default:
		return false
	}
}

func storageObjectCreatorGrants(permission string) bool {
	switch permission {
	case "storage.objects.create", "storage.objects.list",
		"storage.buckets.get":
		return true
	default:
		return false
	}
}

func storageObjectAdminGrants(permission string) bool {
	if storageObjectViewerGrants(permission) || storageObjectCreatorGrants(permission) {
		return true
	}
	switch permission {
	case "storage.objects.delete", "storage.objects.update",
		"storage.objects.getIamPolicy", "storage.objects.setIamPolicy":
		return true
	default:
		return false
	}
}

func artifactregistryReaderGrants(permission string) bool {
	switch permission {
	case "artifactregistry.repositories.get", "artifactregistry.repositories.list",
		"artifactregistry.repositories.downloadArtifacts",
		"artifactregistry.packages.get", "artifactregistry.packages.list",
		"artifactregistry.versions.get", "artifactregistry.versions.list",
		"artifactregistry.files.list", "artifactregistry.tags.list",
		"artifactregistry.dockerimages.get",
		"artifactregistry.repositories.getIamPolicy":
		return true
	default:
		return false
	}
}

func artifactregistryWriterGrants(permission string) bool {
	if artifactregistryReaderGrants(permission) {
		return true
	}
	switch permission {
	case "artifactregistry.repositories.uploadArtifacts",
		"artifactregistry.packages.create", "artifactregistry.versions.create",
		"artifactregistry.dockerimages.create":
		return true
	default:
		return false
	}
}

func artifactregistryRepoAdminGrants(permission string) bool {
	if artifactregistryWriterGrants(permission) {
		return true
	}
	switch permission {
	case "artifactregistry.repositories.create", "artifactregistry.repositories.update",
		"artifactregistry.repositories.delete", "artifactregistry.repositories.setIamPolicy",
		"artifactregistry.packages.delete", "artifactregistry.versions.delete":
		return true
	default:
		return false
	}
}

func datastoreViewerGrants(permission string) bool {
	switch permission {
	case "datastore.entities.get", "datastore.entities.list",
		"datastore.databases.get", "datastore.databases.list":
		return true
	default:
		return false
	}
}

func datastoreUserGrants(permission string) bool {
	if datastoreViewerGrants(permission) {
		return true
	}
	switch permission {
	case "datastore.entities.create", "datastore.entities.update", "datastore.entities.delete":
		return true
	default:
		return false
	}
}

func datastoreOwnerGrants(permission string) bool {
	return strings.HasPrefix(permission, "datastore.") ||
		strings.HasPrefix(permission, "firestore.")
}

func spannerViewerGrants(permission string) bool {
	switch permission {
	case "spanner.instances.get", "spanner.instances.list",
		"spanner.databases.get", "spanner.databases.list",
		"spanner.databases.select", "spanner.databases.read",
		"spanner.sessions.create":
		return true
	default:
		return false
	}
}

func spannerDatabaseUserGrants(permission string) bool {
	if spannerViewerGrants(permission) {
		return true
	}
	return permission == "spanner.databases.write" ||
		permission == "spanner.databases.partitionQuery"
}

func spannerDatabaseAdminGrants(permission string) bool {
	if spannerDatabaseUserGrants(permission) {
		return true
	}
	switch permission {
	case "spanner.databases.create", "spanner.databases.drop",
		"spanner.databases.updateDdl",
		"spanner.instances.create", "spanner.instances.delete":
		return true
	default:
		return false
	}
}

func cloudschedulerViewerGrants(permission string) bool {
	switch permission {
	case "cloudscheduler.jobs.get", "cloudscheduler.jobs.list":
		return true
	default:
		return false
	}
}

func cloudschedulerJobRunnerGrants(permission string) bool {
	if cloudschedulerViewerGrants(permission) {
		return true
	}
	return permission == "cloudscheduler.jobs.run"
}

func eventarcViewerGrants(permission string) bool {
	switch permission {
	case "eventarc.triggers.get", "eventarc.triggers.list",
		"eventarc.channels.get", "eventarc.channels.list":
		return true
	default:
		return false
	}
}

func eventarcDeveloperGrants(permission string) bool {
	if eventarcViewerGrants(permission) {
		return true
	}
	switch permission {
	case "eventarc.triggers.create", "eventarc.triggers.delete", "eventarc.triggers.update",
		"eventarc.channels.create", "eventarc.channels.delete", "eventarc.channels.update":
		return true
	default:
		return false
	}
}

func cloudbuildViewerGrants(permission string) bool {
	switch permission {
	case "cloudbuild.builds.get", "cloudbuild.builds.list",
		"cloudbuild.triggers.get", "cloudbuild.triggers.list",
		"cloudbuild.workerpools.get", "cloudbuild.workerpools.list":
		return true
	default:
		return false
	}
}

func cloudbuildEditorGrants(permission string) bool {
	if cloudbuildViewerGrants(permission) {
		return true
	}
	switch permission {
	case "cloudbuild.builds.create", "cloudbuild.builds.update",
		"cloudbuild.triggers.create", "cloudbuild.triggers.delete", "cloudbuild.triggers.update",
		"cloudbuild.workerpools.create", "cloudbuild.workerpools.use":
		return true
	default:
		return false
	}
}

func firebaseauthViewerGrants(permission string) bool {
	switch permission {
	case "firebaseauth.users.get", "firebaseauth.users.list":
		return true
	default:
		return false
	}
}

func identitytoolkitViewerGrants(permission string) bool {
	switch permission {
	case "identitytoolkit.tenants.get", "identitytoolkit.tenants.list":
		return true
	default:
		return false
	}
}
