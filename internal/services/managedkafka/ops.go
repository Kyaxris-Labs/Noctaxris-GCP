package managedkafka

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
)

const clusterTypeURL = "type.googleapis.com/google.cloud.managedkafka.v1.Cluster"

const locationOperationsPattern = "GET /v1/projects/{project}/locations/{location}/operations/{operation}"

func (s *Service) mountSharedLocationOperations(mux *http.ServeMux, principalFrom principalFunc) {
	restlab.RegisterLocationOperationGetHook(s.tryLocationOperationGet)
	restlab.HandleFuncOnce(mux, locationOperationsPattern, s.wrap(principalFrom, s.locationOperationsGet))
}

func (s *Service) locationOperationsGet(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	opID, _ := splitAction(r.PathValue("operation"))
	if restlab.DispatchLocationOperationGetHooks(w, r, p, project, location, opID) {
		return
	}
	opName := fmt.Sprintf("projects/%s/locations/%s/operations/%s", project, location, opID)
	writeJSON(w, http.StatusOK, map[string]any{
		"name": opName,
		"done": true,
	})
}

func (s *Service) tryLocationOperationGet(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location, opID string) bool {
	clusterID, action, ok := kafkaOperationClusterID(opID)
	if !ok {
		return false
	}
	name := clusterName(project, location, clusterID)
	switch action {
	case "create":
		if _, found, err := s.Store.GetKafkaCluster(name); err != nil || !found {
			return false
		}
	case "delete":
		if _, found, err := s.Store.GetKafkaCluster(name); err == nil && found {
			// Cluster still present; treat as kafka delete poll.
		} else {
			return false
		}
	default:
		return false
	}
	if err := s.require(p, "managedkafka.operations.get", project); err != nil {
		if err2 := s.require(p, "managedkafka.clusters.get", project); err2 != nil {
			writeAuthzErr(w, err)
			return true
		}
	}
	opName := fmt.Sprintf("projects/%s/locations/%s/operations/%s", project, location, opID)
	out := map[string]any{
		"name": opName,
		"done": true,
	}
	if action == "create" {
		if c, found, err := s.Store.GetKafkaCluster(name); err == nil && found {
			out["response"] = withClusterType(toClusterJSON(c))
		}
	}
	writeJSON(w, http.StatusOK, out)
	return true
}

func kafkaOperationClusterID(opID string) (clusterID, action string, ok bool) {
	switch {
	case strings.HasPrefix(opID, "create-"):
		return strings.TrimPrefix(opID, "create-"), "create", true
	case strings.HasPrefix(opID, "delete-"):
		return strings.TrimPrefix(opID, "delete-"), "delete", true
	default:
		return "", "", false
	}
}

func splitAction(seg string) (name, action string) {
	if i := strings.IndexByte(seg, ':'); i >= 0 {
		return seg[:i], seg[i+1:]
	}
	return seg, ""
}

// writeDoneOperation returns a completed LRO so Terraform Managed Kafka waiters
// do not treat the cluster resource name as an unfinished operation.
func writeDoneOperation(w http.ResponseWriter, project, location, opID string, response any) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     fmt.Sprintf("projects/%s/locations/%s/operations/%s", project, location, opID),
		"done":     true,
		"response": response,
	})
}

func withClusterType(m map[string]any) map[string]any {
	m["@type"] = clusterTypeURL
	return m
}
