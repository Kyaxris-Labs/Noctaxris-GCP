package authz

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Policy is a Google IAM Policy allow document (bindings only for lab depth).
type Policy struct {
	Bindings []Binding `json:"bindings"`
	Etag     string    `json:"etag,omitempty"`
}

// Binding maps a role to members.
type Binding struct {
	Role    string   `json:"role"`
	Members []string `json:"members"`
}

// PolicyStore loads IAM policies by resource name.
type PolicyStore interface {
	GetIAMPolicyJSON(resource string) (policyJSON []byte, ok bool, err error)
}

// Evaluator checks permission grants against stored IAM allow policies.
// Root principals bypass evaluation (lab operator convenience; documented in docs/security-defaults.md).
type Evaluator struct {
	Policies PolicyStore
}

// Evaluate returns true when principal may perform permission on resource.
// Deny by default. Root bypasses all checks.
// For nested resources under projects/{id}/..., project-level bindings also apply
// (lab parent inheritance; still fail-closed when neither resource grants).
func (e *Evaluator) Evaluate(principalEmail string, isRoot bool, permission, resource string) (bool, error) {
	if isRoot {
		return true, nil
	}
	if principalEmail == "" || permission == "" || resource == "" {
		return false, nil
	}
	if e.Policies == nil {
		return false, nil
	}
	for _, res := range resourcePolicyChain(resource) {
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
		if roleGrants(b.Role, permission) {
			return true, nil
		}
	}
	return false, nil
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

// EvaluateAny returns true when Evaluate succeeds for any of the resources.
func (e *Evaluator) EvaluateAny(principalEmail string, isRoot bool, permission string, resources ...string) (bool, error) {
	if isRoot {
		return true, nil
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
	for _, m := range members {
		if m == want || m == "allUsers" || m == "allAuthenticatedUsers" {
			return true
		}
	}
	return false
}

// roleGrants is a lab-complete role→permission map for Wave 1 scaffolding.
// roles/owner grants every permission. Other roles grant matching prefixes.
func roleGrants(role, permission string) bool {
	switch role {
	case "roles/owner", "roles/editor":
		return true
	case "roles/viewer":
		return viewerGrants(permission)
	case "roles/iam.securityAdmin":
		return strings.HasPrefix(permission, "iam.") ||
			strings.HasPrefix(permission, "resourcemanager.projects.")
	case "roles/iam.serviceAccountAdmin":
		return strings.HasPrefix(permission, "iam.serviceAccounts.") ||
			strings.HasPrefix(permission, "iam.serviceAccountKeys.")
	case "roles/serviceusage.serviceUsageAdmin":
		return strings.HasPrefix(permission, "serviceusage.")
	default:
		// Custom or service roles: grant when permission shares the service prefix before the first dot pair.
		// Example: roles/storage.admin → storage.*
		if strings.HasPrefix(role, "roles/") {
			rest := strings.TrimPrefix(role, "roles/")
			if i := strings.IndexByte(rest, '.'); i > 0 {
				svc := rest[:i]
				return strings.HasPrefix(permission, svc+".")
			}
		}
		return false
	}
}

// viewerGrants covers get/list (and getIamPolicy) across common lab services.
func viewerGrants(permission string) bool {
	if permission == "" {
		return false
	}
	if strings.HasSuffix(permission, ".get") ||
		strings.HasSuffix(permission, ".list") ||
		strings.HasSuffix(permission, ".getIamPolicy") {
		return true
	}
	// Nested read verbs used by Google APIs (e.g. storage.objects.get, logging.entries.list).
	if strings.Contains(permission, ".get") || strings.Contains(permission, ".list") {
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
		"secretmanager.secrets.get",
		"secretmanager.secrets.list",
		"secretmanager.versions.get",
		"secretmanager.versions.list",
		"secretmanager.versions.access",
		"cloudkms.cryptoKeys.get",
		"cloudkms.cryptoKeys.list",
		"cloudkms.keyRings.get",
		"cloudkms.keyRings.list",
		"logging.logEntries.list",
		"logging.logs.list",
		"run.services.get",
		"run.services.list",
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
		"firestore.documents.list":
		return true
	default:
		return false
	}
}
