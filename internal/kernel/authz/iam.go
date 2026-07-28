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
		return strings.HasSuffix(permission, ".get") ||
			strings.HasSuffix(permission, ".list") ||
			strings.Contains(permission, ".get") ||
			strings.Contains(permission, ".list")
	case "roles/iam.securityAdmin":
		return strings.HasPrefix(permission, "iam.") || strings.HasPrefix(permission, "resourcemanager.projects.")
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
