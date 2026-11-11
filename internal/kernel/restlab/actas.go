package restlab

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// ServiceAccountLookup loads an active service account by email.
// *store.Store implements this.
type ServiceAccountLookup interface {
	GetServiceAccount(email string) (store.ServiceAccount, bool, error)
}

// CheckServiceAccountActAs reports whether p may iam.serviceAccounts.actAs
// the service account identified by email. The resource is
// projects/{home}/serviceAccounts/{email}, where home is that row's project.
// Empty email, a nil lookup, and a nil evaluator deny. A missing account
// denies non-root callers. Root bypasses, matching Evaluator, even when the
// row is absent. Store errors are returned. Parent bindings on the home
// project still apply through Evaluator.Evaluate. The caller project is not a resource.
func CheckServiceAccountActAs(eval *authz.Evaluator, accounts ServiceAccountLookup, p authn.Principal, email string) (bool, error) {
	email = strings.TrimSpace(email)
	if email == "" || accounts == nil || eval == nil {
		return false, nil
	}
	if p.IsRoot {
		return true, nil
	}
	sa, ok, err := accounts.GetServiceAccount(email)
	if err != nil {
		return false, err
	}
	home := strings.TrimSpace(sa.ProjectID)
	addr := strings.TrimSpace(sa.Email)
	if !ok || home == "" || addr == "" {
		return false, nil
	}
	resource := fmt.Sprintf("projects/%s/serviceAccounts/%s", home, addr)
	return eval.Evaluate(p.Email, p.IsRoot, "iam.serviceAccounts.actAs", resource)
}

// RequireServiceAccountActAs checks iam.serviceAccounts.actAs on the service
// account home project. Returns false after writing an error response when
// the check fails. An empty or unknown email is denied.
func RequireServiceAccountActAs(w http.ResponseWriter, eval *authz.Evaluator, accounts ServiceAccountLookup, p authn.Principal, email string) bool {
	ok, err := CheckServiceAccountActAs(eval, accounts, p, email)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return false
	}
	if !ok {
		gcperrors.PermissionDenied(w, "")
		return false
	}
	return true
}
