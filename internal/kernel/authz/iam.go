package authz

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/celutil"
)

// Policy is a Google IAM Policy allow document (bindings only for lab depth).
type Policy struct {
	Bindings []Binding `json:"bindings"`
	Etag     string    `json:"etag,omitempty"`
}

// Binding maps a role to members.
type Binding struct {
	Role      string   `json:"role"`
	Members   []string `json:"members"`
	Condition *Expr    `json:"condition,omitempty"`
}

// Expr is a Google IAM condition (CEL subset in this lab).
type Expr struct {
	Expression  string `json:"expression,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// PolicyStore loads IAM policies by resource name.
type PolicyStore interface {
	GetIAMPolicyJSON(resource string) (policyJSON []byte, ok bool, err error)
}

// RoleStore loads includedPermissions for custom roles (projects/.../roles/... or organizations/.../roles/...).
type RoleStore interface {
	GetRoleIncludedPermissions(roleName string) (perms []string, ok bool, err error)
}

// CRMParentStore resolves Cloud Resource Manager parents for projects and folders.
// Used by Evaluate to walk folder → organization IAM inheritance (GCP resource hierarchy).
type CRMParentStore interface {
	// CRMParent returns the parent resource name (folders/... or organizations/...).
	// ok is false when the resource has no parent (organization) or is unknown.
	CRMParent(resource string) (parent string, ok bool, err error)
}

// Evaluator checks permission grants against stored IAM allow policies.
// Root principals bypass evaluation (lab operator convenience; documented in docs/security-defaults.md).
type Evaluator struct {
	Policies PolicyStore
	Roles    RoleStore
	Parents  CRMParentStore // optional; when set, project/folder Evaluate walks CRM ancestry
	// Now is the lab clock for IAM condition request.time. Nil uses wall UTC.
	Now func() time.Time
}

// Evaluate returns true when principal may perform permission on resource.
// Deny by default. Root bypasses all checks.
// For nested resources under projects/{id}/..., project-level bindings also apply
// (lab parent inheritance; still fail-closed when neither resource grants).
// When Parents is set, project and folder resources also inherit folder and
// organization IAM bindings via CRM ancestry (union of allow policies).
// A nil Evaluator receiver fails closed for non-root (no panic).
func (e *Evaluator) Evaluate(principalEmail string, isRoot bool, permission, resource string) (bool, error) {
	if isRoot {
		return true, nil
	}
	if e == nil {
		return false, nil
	}
	if principalEmail == "" || permission == "" || resource == "" {
		return false, nil
	}
	if e.Policies == nil {
		return false, nil
	}
	chain, err := e.policyChain(resource)
	if err != nil {
		return false, err
	}
	for _, res := range chain {
		ok, err := e.evaluateOnResource(principalEmail, permission, res)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func (e *Evaluator) evaluateOnResource(principalEmail, permission, resource string) (bool, error) {
	raw, ok, err := e.Policies.GetIAMPolicyJSON(resource)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	var pol Policy
	if err := json.Unmarshal(raw, &pol); err != nil {
		return false, fmt.Errorf("parse iam policy for %s: %w", resource, err)
	}
	member := memberIdentity(principalEmail)
	for _, b := range pol.Bindings {
		if !memberIn(b.Members, member) {
			continue
		}
		if !e.conditionAllows(b) {
			continue
		}
		ok, err := e.roleGrants(b.Role, permission)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func (e *Evaluator) conditionAllows(b Binding) bool {
	if b.Condition == nil || strings.TrimSpace(b.Condition.Expression) == "" {
		return true
	}
	now := time.Now().UTC()
	if e != nil && e.Now != nil {
		now = e.Now().UTC()
	}
	return celutil.EvalRequestTime(b.Condition.Expression, now)
}

// resourcePolicyChain returns the resource then its project parent when nested.
func resourcePolicyChain(resource string) []string {
	out := []string{resource}
	const prefix = "projects/"
	if !strings.HasPrefix(resource, prefix) {
		return out
	}
	rest := strings.TrimPrefix(resource, prefix)
	if i := strings.IndexByte(rest, '/'); i > 0 {
		parent := prefix + rest[:i]
		if parent != resource {
			out = append(out, parent)
		}
	}
	return out
}

const crmAncestryMaxDepth = 32

// policyChain returns resource → project (when nested) → folder(s) → organization.
func (e *Evaluator) policyChain(resource string) ([]string, error) {
	out := resourcePolicyChain(resource)
	if e == nil || e.Parents == nil {
		return out, nil
	}
	anchor := ""
	for i := len(out) - 1; i >= 0; i-- {
		if isCRMHierarchyResource(out[i]) {
			anchor = out[i]
			break
		}
	}
	if anchor == "" {
		return out, nil
	}
	seen := make(map[string]struct{}, len(out)+4)
	for _, r := range out {
		seen[r] = struct{}{}
	}
	cur := anchor
	for depth := 0; depth < crmAncestryMaxDepth; depth++ {
		parent, ok, err := e.Parents.CRMParent(cur)
		if err != nil {
			return nil, fmt.Errorf("crm parent for %s: %w", cur, err)
		}
		if !ok || parent == "" {
			break
		}
		if _, dup := seen[parent]; dup {
			break
		}
		out = append(out, parent)
		seen[parent] = struct{}{}
		cur = parent
		if strings.HasPrefix(parent, "organizations/") {
			break
		}
	}
	return out, nil
}

// isCRMHierarchyResource reports bare projects/{id}, folders/{id}, or organizations/{id}.
func isCRMHierarchyResource(resource string) bool {
	for _, prefix := range []string{"projects/", "folders/", "organizations/"} {
		if !strings.HasPrefix(resource, prefix) {
			continue
		}
		rest := strings.TrimPrefix(resource, prefix)
		return rest != "" && !strings.Contains(rest, "/")
	}
	return false
}

// EvaluateAny returns true when Evaluate succeeds for any of the resources.
func (e *Evaluator) EvaluateAny(principalEmail string, isRoot bool, permission string, resources ...string) (bool, error) {
	if isRoot {
		return true, nil
	}
	if e == nil {
		return false, nil
	}
	for _, resource := range resources {
		if resource == "" {
			continue
		}
		ok, err := e.Evaluate(principalEmail, isRoot, permission, resource)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// EvaluateAllUsers reports whether the allUsers principal is granted permission
// on resource (anonymous public access). allAuthenticatedUsers does not match.
func (e *Evaluator) EvaluateAllUsers(permission, resource string) (bool, error) {
	if e == nil || e.Policies == nil || permission == "" || resource == "" {
		return false, nil
	}
	chain, err := e.policyChain(resource)
	if err != nil {
		return false, err
	}
	for _, res := range chain {
		ok, err := e.evaluateExactMemberOnResource("allUsers", permission, res)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// EvaluateAllUsersAny is EvaluateAllUsers across any of the resources.
func (e *Evaluator) EvaluateAllUsersAny(permission string, resources ...string) (bool, error) {
	if e == nil {
		return false, nil
	}
	for _, resource := range resources {
		if resource == "" {
			continue
		}
		ok, err := e.EvaluateAllUsers(permission, resource)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// AllowPrincipalOrAllUsers allows when a principal is granted permission, or when
// unauthenticated allUsers is granted (anonymous dataplane).
func (e *Evaluator) AllowPrincipalOrAllUsers(principalEmail string, isRoot, hasPrincipal bool, permission string, resources ...string) (bool, error) {
	if hasPrincipal {
		return e.EvaluateAny(principalEmail, isRoot, permission, resources...)
	}
	return e.EvaluateAllUsersAny(permission, resources...)
}

func (e *Evaluator) evaluateExactMemberOnResource(member, permission, resource string) (bool, error) {
	raw, ok, err := e.Policies.GetIAMPolicyJSON(resource)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	var pol Policy
	if err := json.Unmarshal(raw, &pol); err != nil {
		return false, fmt.Errorf("parse iam policy for %s: %w", resource, err)
	}
	for _, b := range pol.Bindings {
		if !exactMemberIn(b.Members, member) {
			continue
		}
		if !e.conditionAllows(b) {
			continue
		}
		ok, err := e.roleGrants(b.Role, permission)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func exactMemberIn(members []string, want string) bool {
	for _, m := range members {
		if m == want {
			return true
		}
	}
	return false
}

// TestIamPermissions returns the subset of permissions granted to the principal.
func (e *Evaluator) TestIamPermissions(principalEmail string, isRoot bool, resource string, permissions []string) ([]string, error) {
	out := make([]string, 0, len(permissions))
	for _, p := range permissions {
		ok, err := e.Evaluate(principalEmail, isRoot, p, resource)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, p)
		}
	}
	return out, nil
}

// TestIamPermissionsAny returns permissions granted on any of the resources.
func (e *Evaluator) TestIamPermissionsAny(principalEmail string, isRoot bool, resources []string, permissions []string) ([]string, error) {
	out := make([]string, 0, len(permissions))
	for _, p := range permissions {
		ok, err := e.EvaluateAny(principalEmail, isRoot, p, resources...)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func memberIdentity(email string) string {
	email = strings.TrimSpace(email)
	if strings.Contains(email, ":") {
		return email
	}
	return "serviceAccount:" + email
}

func memberIn(members []string, want string) bool {
	// Identity Toolkit principals (user:...) match only exact member bindings.
	// They must not satisfy allAuthenticatedUsers / allUsers project grants.
	toolkitPrincipal := strings.HasPrefix(want, "user:")
	for _, m := range members {
		if m == want {
			return true
		}
		if toolkitPrincipal {
			continue
		}
		if m == "allUsers" || m == "allAuthenticatedUsers" {
			return true
		}
	}
	return false
}

// roleGrants is a lab-complete role→permission map for seeded IAM roles.
// roles/owner grants every permission. roles/editor grants mutators except IAM
// admin / impersonation, secret payload access, KMS crypto ops, and orgpolicy
// mutate. roles/viewer is read-only metadata (no secret payload access).
//
// Marketed predefined roles use explicit permission sets. The residual
// {svc}.* prefix allowlist is empty, so unknown roles (for example
// roles/xyz.admin) fail closed. Custom project/org roles honor
// includedPermissions from RoleStore when present.
func (e *Evaluator) roleGrants(role, permission string) (bool, error) {
	switch role {
	case "roles/owner":
		return true, nil
	case "roles/editor":
		return editorGrants(permission), nil
	case "roles/viewer":
		return viewerGrants(permission), nil
	case "roles/iam.securityAdmin":
		return securityAdminGrants(permission), nil
	case "roles/iam.serviceAccountAdmin":
		return serviceAccountAdminGrants(permission), nil
	case "roles/iam.serviceAccountTokenCreator":
		return tokenCreatorGrants(permission), nil
	case "roles/serviceusage.serviceUsageAdmin":
		return strings.HasPrefix(permission, "serviceusage."), nil
	case "roles/run.invoker":
		return permission == "run.routes.invoke", nil
	case "roles/cloudfunctions.invoker":
		return permission == "cloudfunctions.functions.invoke", nil
	case "roles/cloudbuild.workerPoolUser":
		return permission == "cloudbuild.workerpools.use" || permission == "cloudbuild.workerpools.get", nil
	case "roles/logging.viewAccessor":
		return permission == "logging.views.get" || permission == "logging.views.list", nil
	case "roles/secretmanager.viewer":
		return secretmanagerViewerGrants(permission), nil
	case "roles/secretmanager.secretAccessor":
		return secretmanagerSecretAccessorGrants(permission), nil
	case "roles/secretmanager.secretVersionManager":
		return secretmanagerSecretVersionManagerGrants(permission), nil
	case "roles/secretmanager.admin":
		return strings.HasPrefix(permission, "secretmanager."), nil
	case "roles/cloudkms.viewer":
		return cloudkmsViewerGrants(permission), nil
	case "roles/cloudkms.admin":
		return cloudkmsAdminGrants(permission), nil
	case "roles/cloudkms.cryptoKeyEncrypterDecrypter":
		return permission == "cloudkms.cryptoKeyVersions.useToEncrypt" ||
			permission == "cloudkms.cryptoKeyVersions.useToDecrypt" ||
			permission == "cloudkms.cryptoKeys.get", nil
	case "roles/cloudkms.cryptoKeyEncrypter":
		return permission == "cloudkms.cryptoKeyVersions.useToEncrypt" ||
			permission == "cloudkms.cryptoKeys.get", nil
	case "roles/cloudkms.cryptoKeyDecrypter":
		return permission == "cloudkms.cryptoKeyVersions.useToDecrypt" ||
			permission == "cloudkms.cryptoKeys.get", nil
	case "roles/cloudkms.signer", "roles/cloudkms.signerVerifier":
		return permission == "cloudkms.cryptoKeyVersions.useToSign" ||
			permission == "cloudkms.cryptoKeyVersions.viewPublicKey" ||
			permission == "cloudkms.cryptoKeys.get", nil
	case "roles/cloudkms.publicKeyViewer":
		return permission == "cloudkms.cryptoKeyVersions.viewPublicKey" ||
			permission == "cloudkms.cryptoKeys.get", nil
	default:
		if isCustomRoleName(role) {
			if e == nil || e.Roles == nil {
				return false, nil
			}
			perms, ok, err := e.Roles.GetRoleIncludedPermissions(role)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
			for _, p := range perms {
				if p == permission {
					return true, nil
				}
			}
			return false, nil
		}
		// Explicit permission sets for marketed predefined roles.
		if granted, known := predefinedServiceRoleGrants(role, permission); known {
			return granted, nil
		}
		// Residual {svc}.* allowlist (empty): unknown roles/xyz.* fail closed.
		if strings.HasPrefix(role, "roles/") {
			rest := strings.TrimPrefix(role, "roles/")
			if i := strings.IndexByte(rest, '.'); i > 0 {
				svc := rest[:i]
				if labPredefinedServicePrefixes[svc] {
					return strings.HasPrefix(permission, svc+"."), nil
				}
			}
		}
		return false, nil
	}
}

// isCustomRoleName matches projects/{id}/roles/{roleId} or organizations/{id}/roles/{roleId}.
func isCustomRoleName(role string) bool {
	return (strings.HasPrefix(role, "projects/") && strings.Contains(role, "/roles/")) ||
		(strings.HasPrefix(role, "organizations/") && strings.Contains(role, "/roles/"))
}

// labPredefinedServicePrefixes is the residual allowlist for roles/{svc}.* →
// {svc}.* grants. Marketed lab services use explicit maps in
// predefinedServiceRoleGrants (and the Secret Manager / Cloud KMS cases above).
// An empty map means unknown roles/xyz.* fail closed with no prefix shortcut.
var labPredefinedServicePrefixes = map[string]bool{}

// tokenCreatorGrants mirrors roles/iam.serviceAccountTokenCreator (impersonation).
func tokenCreatorGrants(permission string) bool {
	switch permission {
	case "iam.serviceAccounts.getAccessToken",
		"iam.serviceAccounts.actAs",
		"iam.serviceAccounts.signBlob",
		"iam.serviceAccounts.signJwt",
		"iam.serviceAccounts.implicitDelegation",
		"iam.serviceAccounts.generateAccessToken",
		"iam.serviceAccounts.generateIdToken":
		return true
	default:
		return false
	}
}

// serviceAccountAdminGrants mirrors roles/iam.serviceAccountAdmin: manage SAs and
// keys without TokenCreator / actAs / mint / sign permissions.
func serviceAccountAdminGrants(permission string) bool {
	switch permission {
	case "iam.serviceAccounts.create",
		"iam.serviceAccounts.delete",
		"iam.serviceAccounts.disable",
		"iam.serviceAccounts.enable",
		"iam.serviceAccounts.get",
		"iam.serviceAccounts.getIamPolicy",
		"iam.serviceAccounts.list",
		"iam.serviceAccounts.setIamPolicy",
		"iam.serviceAccounts.undelete",
		"iam.serviceAccounts.update",
		"iam.serviceAccountKeys.create",
		"iam.serviceAccountKeys.delete",
		"iam.serviceAccountKeys.disable",
		"iam.serviceAccountKeys.enable",
		"iam.serviceAccountKeys.get",
		"iam.serviceAccountKeys.list":
		return true
	default:
		return false
	}
}

// securityAdminGrants mirrors roles/iam.securityAdmin: IAM policy and role admin
// without TokenCreator / actAs / mint / sign permissions.
func securityAdminGrants(permission string) bool {
	switch permission {
	case "iam.roles.create",
		"iam.roles.delete",
		"iam.roles.get",
		"iam.roles.list",
		"iam.roles.undelete",
		"iam.roles.update",
		"iam.serviceAccounts.get",
		"iam.serviceAccounts.list",
		"iam.serviceAccounts.getIamPolicy",
		"iam.serviceAccounts.setIamPolicy",
		"iam.serviceAccountKeys.get",
		"iam.serviceAccountKeys.list",
		"iam.workloadIdentityPools.create",
		"iam.workloadIdentityPools.delete",
		"iam.workloadIdentityPools.get",
		"iam.workloadIdentityPools.list",
		"iam.workloadIdentityPools.update",
		"iam.workloadIdentityPoolProviders.create",
		"iam.workloadIdentityPoolProviders.delete",
		"iam.workloadIdentityPoolProviders.get",
		"iam.workloadIdentityPoolProviders.list",
		"iam.workloadIdentityPoolProviders.update",
		"resourcemanager.projects.get",
		"resourcemanager.projects.list",
		"resourcemanager.projects.getIamPolicy",
		"resourcemanager.projects.setIamPolicy",
		"resourcemanager.folders.get",
		"resourcemanager.folders.getIamPolicy",
		"resourcemanager.folders.setIamPolicy",
		"resourcemanager.organizations.get",
		"resourcemanager.organizations.getIamPolicy",
		"resourcemanager.organizations.setIamPolicy":
		return true
	default:
		// Cross-service IAM policy admin without impersonation wildcards.
		if strings.HasSuffix(permission, ".getIamPolicy") || strings.HasSuffix(permission, ".setIamPolicy") {
			return true
		}
		return false
	}
}

// editorGrants mirrors GCP roles/editor: broad mutate except IAM policy admin,
// service-account impersonation, Secret Manager payload access, Cloud KMS
// cryptographic ops, and Organization Policy mutate.
func editorGrants(permission string) bool {
	if permission == "" {
		return false
	}
	if strings.HasSuffix(permission, ".setIamPolicy") {
		return false
	}
	switch permission {
	case "iam.serviceAccounts.getAccessToken",
		"iam.serviceAccounts.actAs",
		"iam.serviceAccounts.signBlob",
		"iam.serviceAccounts.signJwt",
		"iam.serviceAccounts.implicitDelegation",
		"iam.serviceAccounts.generateAccessToken",
		"iam.serviceAccounts.generateIdToken",
		"secretmanager.versions.access",
		"cloudkms.cryptoKeyVersions.useToEncrypt",
		"cloudkms.cryptoKeyVersions.useToDecrypt",
		"cloudkms.cryptoKeyVersions.useToSign",
		"cloudkms.cryptoKeyVersions.viewPublicKey",
		"orgpolicy.policies.create",
		"orgpolicy.policies.update",
		"orgpolicy.policies.delete":
		return false
	default:
		return true
	}
}

// viewerGrants covers get/list (and getIamPolicy) across common lab services.
// Uses suffix matches only — never strings.Contains(".get"), which would grant
// iam.serviceAccounts.getAccessToken and similar.
func viewerGrants(permission string) bool {
	if permission == "" {
		return false
	}
	if strings.HasSuffix(permission, ".get") ||
		strings.HasSuffix(permission, ".list") ||
		strings.HasSuffix(permission, ".getIamPolicy") ||
		strings.HasSuffix(permission, ".search") {
		return true
	}
	switch permission {
	case "resourcemanager.projects.get",
		"resourcemanager.projects.getIamPolicy",
		"resourcemanager.projects.list",
		"resourcemanager.projects.search",
		"iam.serviceAccounts.get",
		"iam.serviceAccounts.list",
		"iam.serviceAccountKeys.get",
		"iam.serviceAccountKeys.list",
		"serviceusage.services.get",
		"serviceusage.services.list",
		"storage.buckets.get",
		"storage.buckets.list",
		"storage.objects.get",
		"storage.objects.list",
		"pubsub.topics.get",
		"pubsub.topics.list",
		"pubsub.subscriptions.get",
		"pubsub.subscriptions.list",
		"pubsub.snapshots.get",
		"pubsub.snapshots.list",
		"secretmanager.secrets.get",
		"secretmanager.secrets.list",
		"secretmanager.versions.get",
		"secretmanager.versions.list",
		"cloudkms.cryptoKeys.get",
		"cloudkms.cryptoKeys.list",
		"cloudkms.keyRings.get",
		"cloudkms.keyRings.list",
		"logging.logEntries.list",
		"logging.logs.list",
		"run.services.get",
		"run.services.list",
		"run.revisions.get",
		"run.revisions.list",
		"cloudfunctions.functions.get",
		"cloudfunctions.functions.list",
		"cloudscheduler.jobs.get",
		"cloudscheduler.jobs.list",
		"cloudtasks.queues.get",
		"cloudtasks.queues.list",
		"cloudtasks.tasks.get",
		"cloudtasks.tasks.list",
		"bigquery.datasets.get",
		"bigquery.datasets.list",
		"bigquery.tables.get",
		"bigquery.tables.list",
		"monitoring.metricDescriptors.get",
		"monitoring.metricDescriptors.list",
		"monitoring.timeSeries.list",
		"datastore.entities.get",
		"datastore.entities.list",
		"eventarc.triggers.get",
		"eventarc.triggers.list",
		"firestore.documents.get",
		"firestore.documents.list",
		"cloudasset.assets.searchAllResources",
		"cloudasset.assets.listResource",
		"cloudasset.feeds.get",
		"cloudasset.feeds.list":
		return true
	default:
		return false
	}
}

func secretmanagerViewerGrants(permission string) bool {
	switch permission {
	case "secretmanager.secrets.get",
		"secretmanager.secrets.list",
		"secretmanager.versions.get",
		"secretmanager.versions.list",
		"secretmanager.secrets.getIamPolicy",
		"secretmanager.locations.get",
		"secretmanager.locations.list":
		return true
	default:
		return false
	}
}

func secretmanagerSecretAccessorGrants(permission string) bool {
	switch permission {
	case "secretmanager.versions.access",
		"secretmanager.versions.get",
		"secretmanager.versions.list",
		"secretmanager.secrets.get",
		"secretmanager.secrets.list":
		return true
	default:
		return false
	}
}

func secretmanagerSecretVersionManagerGrants(permission string) bool {
	switch permission {
	case "secretmanager.versions.add",
		"secretmanager.versions.enable",
		"secretmanager.versions.disable",
		"secretmanager.versions.destroy",
		"secretmanager.versions.get",
		"secretmanager.versions.list",
		"secretmanager.secrets.get",
		"secretmanager.secrets.list":
		return true
	default:
		return false
	}
}

func cloudkmsViewerGrants(permission string) bool {
	switch permission {
	case "cloudkms.keyRings.get",
		"cloudkms.keyRings.list",
		"cloudkms.cryptoKeys.get",
		"cloudkms.cryptoKeys.list",
		"cloudkms.cryptoKeyVersions.get",
		"cloudkms.cryptoKeyVersions.list",
		"cloudkms.cryptoKeys.getIamPolicy",
		"cloudkms.locations.get",
		"cloudkms.locations.list":
		return true
	default:
		return false
	}
}

// cloudkmsAdminGrants mirrors Cloud KMS Admin: manage keys without cryptographic ops.
func cloudkmsAdminGrants(permission string) bool {
	if permission == "cloudkms.cryptoKeyVersions.useToEncrypt" ||
		permission == "cloudkms.cryptoKeyVersions.useToDecrypt" ||
		permission == "cloudkms.cryptoKeyVersions.useToSign" ||
		permission == "cloudkms.cryptoKeyVersions.viewPublicKey" {
		return false
	}
	return strings.HasPrefix(permission, "cloudkms.")
}
