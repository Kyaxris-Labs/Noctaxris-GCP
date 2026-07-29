package managedkafka

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func aclName(project, location, clusterID, aclID string) string {
	return clusterName(project, location, clusterID) + "/acls/" + aclID
}

// deriveACLPattern maps GCP Managed Kafka aclId shapes to resource fields.
// See https://cloud.google.com/managed-kafka/docs/reference/rest/v1/projects.locations.clusters.acls
func deriveACLPattern(aclID string) (resourceType, resourceName, patternType string) {
	switch {
	case aclID == "cluster":
		return "CLUSTER", "kafka-cluster", "LITERAL"
	case aclID == "allTopics":
		return "TOPIC", "*", "LITERAL"
	case aclID == "allConsumerGroups":
		return "GROUP", "*", "LITERAL"
	case aclID == "allTransactionalIds":
		return "TRANSACTIONAL_ID", "*", "LITERAL"
	case strings.HasPrefix(aclID, "topicPrefixed/"):
		return "TOPIC", strings.TrimPrefix(aclID, "topicPrefixed/"), "PREFIXED"
	case strings.HasPrefix(aclID, "consumerGroupPrefixed/"):
		return "GROUP", strings.TrimPrefix(aclID, "consumerGroupPrefixed/"), "PREFIXED"
	case strings.HasPrefix(aclID, "transactionalIdPrefixed/"):
		return "TRANSACTIONAL_ID", strings.TrimPrefix(aclID, "transactionalIdPrefixed/"), "PREFIXED"
	case strings.HasPrefix(aclID, "topic/"):
		return "TOPIC", strings.TrimPrefix(aclID, "topic/"), "LITERAL"
	case strings.HasPrefix(aclID, "consumerGroup/"):
		return "GROUP", strings.TrimPrefix(aclID, "consumerGroup/"), "LITERAL"
	case strings.HasPrefix(aclID, "transactionalId/"):
		return "TRANSACTIONAL_ID", strings.TrimPrefix(aclID, "transactionalId/"), "LITERAL"
	default:
		return "", aclID, "LITERAL"
	}
}

func (s *Service) createACL(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	clusterID := r.PathValue("cluster")
	if err := s.require(p, "managedkafka.acls.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if !restlab.RequireServiceEnabled(w, s.Store, project, "managedkafka.googleapis.com") {
		return
	}
	parent := clusterName(project, location, clusterID)
	if _, ok, err := s.Store.GetKafkaCluster(parent); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Cluster not found")
		return
	}

	aclID := r.URL.Query().Get("aclId")
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && r.ContentLength != 0 {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if aclID == "" && body != nil {
		if id, ok := body["name"].(string); ok && id != "" {
			const marker = "/acls/"
			if i := strings.Index(id, marker); i >= 0 {
				aclID = id[i+len(marker):]
			} else {
				parts := strings.Split(id, "/")
				aclID = parts[len(parts)-1]
			}
		}
	}
	if aclID == "" {
		gcperrors.InvalidArgument(w, "aclId query parameter is required")
		return
	}

	entriesJSON := "[]"
	if entries, ok := body["aclEntries"]; ok {
		raw, err := json.Marshal(entries)
		if err != nil {
			gcperrors.InvalidArgument(w, "invalid aclEntries")
			return
		}
		entriesJSON = string(raw)
	}
	rt, rn, pt := deriveACLPattern(aclID)
	name := aclName(project, location, clusterID, aclID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateKafkaACL(store.KafkaACL{
		Name: name, ClusterName: parent, ProjectID: project, Location: location,
		ClusterID: clusterID, ACLID: aclID, ResourceType: rt, ResourceName: rn,
		PatternType: pt, ACLEntriesJSON: entriesJSON, Etag: "ACAB", CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "acl already exists")
		return
	}
	out, ok, err := s.Store.GetKafkaACL(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created acl missing")
		return
	}
	writeJSON(w, http.StatusOK, toACLJSON(out))
}

func (s *Service) getACL(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	clusterID := r.PathValue("cluster")
	aclID := r.PathValue("acl")
	if err := s.require(p, "managedkafka.acls.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	a, ok, err := s.Store.GetKafkaACL(aclName(project, location, clusterID, aclID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Acl not found")
		return
	}
	writeJSON(w, http.StatusOK, toACLJSON(a))
}

func (s *Service) listACLs(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	clusterID := r.PathValue("cluster")
	if err := s.require(p, "managedkafka.acls.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	parent := clusterName(project, location, clusterID)
	if _, ok, err := s.Store.GetKafkaCluster(parent); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Cluster not found")
		return
	}
	list, err := s.Store.ListKafkaACLs(parent)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, a := range list {
		items = append(items, toACLJSON(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"acls": items})
}

func (s *Service) deleteACL(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	clusterID := r.PathValue("cluster")
	aclID := r.PathValue("acl")
	if err := s.require(p, "managedkafka.acls.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	_, ok, err := s.Store.DeleteKafkaACL(aclName(project, location, clusterID, aclID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Acl not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func toACLJSON(a store.KafkaACL) map[string]any {
	var entries any = []any{}
	_ = json.Unmarshal([]byte(a.ACLEntriesJSON), &entries)
	out := map[string]any{
		"name":       a.Name,
		"aclEntries": entries,
		"etag":       a.Etag,
	}
	if a.ResourceType != "" {
		out["resourceType"] = a.ResourceType
	}
	if a.ResourceName != "" {
		out["resourceName"] = a.ResourceName
	}
	if a.PatternType != "" {
		out["patternType"] = a.PatternType
	}
	return out
}
