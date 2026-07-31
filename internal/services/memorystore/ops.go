package memorystore

import (
	"fmt"
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
)

const instanceTypeURL = "type.googleapis.com/google.cloud.redis.v1.Instance"

const locationOperationsPattern = "GET /v1/projects/{project}/locations/{location}/operations/{operation}"

// mountOperations registers lab Operations.get. Shared with Certificate Manager
// on the same ServeMux pattern; first Mount wins via restlab.HandleFuncOnce.
func (s *Service) mountOperations(mux *http.ServeMux, principalFrom principalFunc) {
	restlab.HandleFuncOnce(mux, locationOperationsPattern, s.wrap(principalFrom, s.getOperation))
}

func (s *Service) getOperation(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	opID, _ := splitAction(r.PathValue("operation"))
	if restlab.DispatchLocationOperationGetHooks(w, r, p, project, location, opID) {
		return
	}
	// Prefer redis.operations.get; instances.get is an accepted lab fallback.
	if err := s.require(p, "redis.operations.get", project); err != nil {
		if err2 := s.require(p, "redis.instances.get", project); err2 != nil {
			writeAuthzErr(w, err)
			return
		}
	}
	opName := fmt.Sprintf("projects/%s/locations/%s/operations/%s", project, location, opID)
	writeJSON(w, http.StatusOK, map[string]any{
		"name": opName,
		"done": true,
	})
}

// writeDoneOperation returns a completed LRO so Terraform Redis waiters
// do not treat the instance resource name as an unfinished operation.
func writeDoneOperation(w http.ResponseWriter, project, location, opID string, response any) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     fmt.Sprintf("projects/%s/locations/%s/operations/%s", project, location, opID),
		"done":     true,
		"response": response,
	})
}
