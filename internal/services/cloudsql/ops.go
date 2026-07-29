package cloudsql

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
)

func (s *Service) mountOperations(mux *http.ServeMux, principalFrom principalFunc, prefix string) {
	mux.HandleFunc("GET "+prefix+"/projects/{project}/operations", s.wrap(principalFrom, s.listOperations))
	mux.HandleFunc("GET "+prefix+"/projects/{project}/operations/{operation}", s.wrap(principalFrom, s.getOperation))
}

// getOperation returns a completed sql#operation (Filestore-style immediate DONE theatre).
func (s *Service) getOperation(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	opID := r.PathValue("operation")
	if err := s.require(p, "cloudsql.operations.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sqlOperationFromID(opID, project))
}

func (s *Service) listOperations(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "cloudsql.operations.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":  "sql#operationsList",
		"items": []any{},
	})
}

func synthesizeOpID(operationType, targetID string) string {
	typ := strings.ToLower(strings.ReplaceAll(operationType, "_", "-"))
	if typ == "" {
		typ = "op"
	}
	if targetID == "" {
		return typ
	}
	return typ + "-" + targetID
}

func sqlOperationJSON(operationType, targetID, project string) map[string]any {
	opID := synthesizeOpID(operationType, targetID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return map[string]any{
		"kind":          "sql#operation",
		"name":          opID,
		"status":        "DONE",
		"operationType": operationType,
		"targetId":      targetID,
		"targetProject": project,
		"insertTime":    now,
		"startTime":     now,
		"endTime":       now,
		"selfLink": fmt.Sprintf("https://sqladmin.googleapis.com/sql/v1/projects/%s/operations/%s",
			project, opID),
		"targetLink": fmt.Sprintf("https://sqladmin.googleapis.com/sql/v1/projects/%s/instances/%s",
			project, targetID),
	}
}

// sqlOperationFromID synthesizes a DONE operation for Operations.get polling.
func sqlOperationFromID(opID, project string) map[string]any {
	opType, targetID := parseSynthesizedOpID(opID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	out := map[string]any{
		"kind":          "sql#operation",
		"name":          opID,
		"status":        "DONE",
		"operationType": opType,
		"targetProject": project,
		"insertTime":    now,
		"startTime":     now,
		"endTime":       now,
		"selfLink": fmt.Sprintf("https://sqladmin.googleapis.com/sql/v1/projects/%s/operations/%s",
			project, opID),
	}
	if targetID != "" {
		out["targetId"] = targetID
		out["targetLink"] = fmt.Sprintf("https://sqladmin.googleapis.com/sql/v1/projects/%s/instances/%s",
			project, targetID)
	}
	return out
}

func parseSynthesizedOpID(opID string) (operationType, targetID string) {
	known := []string{
		"create-user-", "delete-user-", "create-database-", "delete-database-",
		"create-", "delete-", "update-",
	}
	lower := strings.ToLower(opID)
	for _, p := range known {
		if strings.HasPrefix(lower, p) {
			typ := strings.ToUpper(strings.ReplaceAll(strings.TrimSuffix(p, "-"), "-", "_"))
			return typ, opID[len(p):]
		}
	}
	return "UPDATE", opID
}
