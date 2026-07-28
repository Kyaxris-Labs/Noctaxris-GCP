// Package restlab provides shared REST authn/authz helpers for lab service handlers.
package restlab

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
)

// PrincipalFrom extracts an authenticated principal from the request.
type PrincipalFrom = func(*http.Request) (authn.Principal, bool)

// Handler is an HTTP handler that receives an authenticated principal.
type Handler = func(w http.ResponseWriter, r *http.Request, p authn.Principal)

// ErrDenied is returned by Require/Evaluate when the principal lacks permission.
var ErrDenied = fmt.Errorf("permission denied")

// Wrap returns an http.HandlerFunc that requires authentication via principalFrom.
func Wrap(principalFrom PrincipalFrom, h Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := principalFrom(r)
		if !ok {
			gcperrors.Unauthenticated(w, "")
			return
		}
		h(w, r, p)
	}
}

// Evaluate checks permission on resource and returns ErrDenied when denied.
func Evaluate(eval *authz.Evaluator, p authn.Principal, permission, resource string) error {
	ok, err := eval.Evaluate(p.Email, p.IsRoot, permission, resource)
	if err != nil {
		return err
	}
	if !ok {
		return ErrDenied
	}
	return nil
}

// Require checks permission on projects/{projectID} and returns ErrDenied when denied.
func Require(eval *authz.Evaluator, p authn.Principal, permission, projectID string) error {
	return Evaluate(eval, p, permission, "projects/"+projectID)
}

// WriteJSON writes v as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteAuthzErr writes PermissionDenied for ErrDenied, otherwise Internal.
func WriteAuthzErr(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrDenied) {
		gcperrors.PermissionDenied(w, "")
		return
	}
	gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
}
