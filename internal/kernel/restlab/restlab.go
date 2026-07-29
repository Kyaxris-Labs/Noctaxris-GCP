// Package restlab provides shared REST authn/authz helpers for lab service handlers.
package restlab

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// handleFuncOnceKeys tracks patterns already registered per ServeMux so shared
// lab paths (for example location Operations.get) can be claimed by the first
// service without panicking on a second Mount.
var (
	handleFuncOnceMu   sync.Mutex
	handleFuncOnceKeys = map[string]struct{}{}
)

// ServiceUsageChecker looks up whether a project API is ENABLED.
type ServiceUsageChecker interface {
	IsServiceEnabled(projectID, serviceName string) (bool, error)
}

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

// HandleFuncOnce registers pattern on mux only once per process for that mux.
// Later callers with the same mux+pattern are no-ops (first handler wins).
func HandleFuncOnce(mux *http.ServeMux, pattern string, handler http.HandlerFunc) {
	key := fmt.Sprintf("%p\n%s", mux, pattern)
	handleFuncOnceMu.Lock()
	defer handleFuncOnceMu.Unlock()
	if _, ok := handleFuncOnceKeys[key]; ok {
		return
	}
	handleFuncOnceKeys[key] = struct{}{}
	mux.HandleFunc(pattern, handler)
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

// ServiceDisabledMessage is the FailedPrecondition text when an API is DISABLED.
func ServiceDisabledMessage(serviceName string) string {
	return fmt.Sprintf("Service %s is disabled for this project.", serviceName)
}

// CheckServiceEnabled returns nil when the API is ENABLED.
// When DISABLED it returns store.ErrServiceDisabled wrapped with the service name.
func CheckServiceEnabled(st ServiceUsageChecker, projectID, serviceName string) error {
	enabled, err := st.IsServiceEnabled(projectID, serviceName)
	if err != nil {
		return err
	}
	if !enabled {
		return fmt.Errorf("%w: %s", store.ErrServiceDisabled, serviceName)
	}
	return nil
}

// RequireServiceEnabled writes FailedPrecondition when the API is DISABLED (or Internal on lookup error).
// Returns false when the handler should stop.
func RequireServiceEnabled(w http.ResponseWriter, st ServiceUsageChecker, projectID, serviceName string) bool {
	err := CheckServiceEnabled(st, projectID, serviceName)
	if err == nil {
		return true
	}
	if errors.Is(err, store.ErrServiceDisabled) {
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, ServiceDisabledMessage(serviceName))
		return false
	}
	gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
	return false
}
