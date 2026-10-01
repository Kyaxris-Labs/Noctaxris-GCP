package restlab

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
)

// RequireServiceAccountActAs checks iam.serviceAccounts.actAs on the named SA
// (and the parent project). Empty email is allowed (caller skips impersonation).
// Returns false after writing an error response when the check fails.
func RequireServiceAccountActAs(w http.ResponseWriter, eval *authz.Evaluator, p authn.Principal, project, email string) bool {
	email = strings.TrimSpace(email)
	if email == "" {
		return true
	}
	project = strings.TrimSpace(project)
	if project == "" || eval == nil {
		gcperrors.PermissionDenied(w, "")
		return false
	}
	saRes := fmt.Sprintf("projects/%s/serviceAccounts/%s", project, email)
	ok, err := eval.EvaluateAny(p.Email, p.IsRoot, "iam.serviceAccounts.actAs", saRes, "projects/"+project)
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
