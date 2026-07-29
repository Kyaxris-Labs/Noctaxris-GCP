package managedkafka

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func topicName(project, location, clusterID, topicID string) string {
	return clusterName(project, location, clusterID) + "/topics/" + topicID
}

func (s *Service) createTopic(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	clusterID := r.PathValue("cluster")
	if err := s.require(p, "managedkafka.topics.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if !restlab.RequireServiceEnabled(w, s.Store, project, "managedkafka.googleapis.com") {
		return
	}
	parent := clusterName(project, location, clusterID)
	cluster, ok, err := s.Store.GetKafkaCluster(parent)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Cluster not found")
		return
	}

	topicID := r.URL.Query().Get("topicId")
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && r.ContentLength != 0 {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if topicID == "" && body != nil {
		if id, ok := body["name"].(string); ok && id != "" {
			parts := strings.Split(id, "/")
			topicID = parts[len(parts)-1]
		}
	}
	if topicID == "" {
		gcperrors.InvalidArgument(w, "topicId query parameter is required")
		return
	}

	partitions := 1
	if v, ok := asInt(body["partitionCount"]); ok && v > 0 {
		partitions = v
	}
	replication := 1
	if v, ok := asInt(body["replicationFactor"]); ok && v > 0 {
		replication = v
	}
	configsJSON := "{}"
	if configs, ok := body["configs"]; ok {
		raw, _ := json.Marshal(configs)
		configsJSON = string(raw)
	}

	name := topicName(project, location, clusterID, topicID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateKafkaTopic(store.KafkaTopic{
		Name: name, ClusterName: parent, ProjectID: project, Location: location,
		ClusterID: clusterID, TopicID: topicID, PartitionCount: partitions,
		ReplicationFactor: replication, ConfigsJSON: configsJSON, CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "topic already exists")
		return
	}

	s.tryNestedTopicCreate(r.Context(), cluster, topicID, partitions, replication)

	out, ok, err := s.Store.GetKafkaTopic(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created topic missing")
		return
	}
	writeJSON(w, http.StatusOK, toTopicJSON(out))
}

func (s *Service) tryNestedTopicCreate(ctx context.Context, cluster store.KafkaCluster, topicID string, partitions, replication int) {
	if strings.TrimSpace(cluster.ContainerID) == "" {
		return
	}
	cli, owned, err := s.nestEngine()
	if err != nil || cli == nil || !cli.Enabled() {
		return
	}
	if owned {
		defer func() { _ = cli.Close() }()
	}
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_ = cli.CreateRedpandaTopic(runCtx, cluster.ContainerID, topicID, partitions, replication)
}

func (s *Service) getTopic(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	clusterID := r.PathValue("cluster")
	topicID := r.PathValue("topic")
	if err := s.require(p, "managedkafka.topics.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	t, ok, err := s.Store.GetKafkaTopic(topicName(project, location, clusterID, topicID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Topic not found")
		return
	}
	writeJSON(w, http.StatusOK, toTopicJSON(t))
}

func (s *Service) listTopics(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	clusterID := r.PathValue("cluster")
	if err := s.require(p, "managedkafka.topics.list", project); err != nil {
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
	list, err := s.Store.ListKafkaTopics(parent)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, t := range list {
		items = append(items, toTopicJSON(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"topics": items})
}

func (s *Service) deleteTopic(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	clusterID := r.PathValue("cluster")
	topicID := r.PathValue("topic")
	if err := s.require(p, "managedkafka.topics.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	_, ok, err := s.Store.DeleteKafkaTopic(topicName(project, location, clusterID, topicID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Topic not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func toTopicJSON(t store.KafkaTopic) map[string]any {
	var configs any = map[string]string{}
	_ = json.Unmarshal([]byte(t.ConfigsJSON), &configs)
	return map[string]any{
		"name":              t.Name,
		"partitionCount":    t.PartitionCount,
		"replicationFactor": t.ReplicationFactor,
		"configs":           configs,
	}
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}
