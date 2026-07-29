package managedkafka

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// DefaultLocation is the lab default Managed Kafka region.
const DefaultLocation = "us-central1"

// Service serves Managed Kafka REST v1 (clusters CRUD lite).
type Service struct {
	Store          *store.Store
	Authz          *authz.Evaluator
	DockerHost     string
	DockerCertPath string
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers location-scoped cluster routes.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/clusters", s.wrap(principalFrom, s.listClusters))
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/clusters", s.wrap(principalFrom, s.createCluster))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/clusters/{cluster}", s.wrap(principalFrom, s.getCluster))
	mux.HandleFunc("DELETE /v1/projects/{project}/locations/{location}/clusters/{cluster}", s.wrap(principalFrom, s.deleteCluster))
}

type handlerFunc func(w http.ResponseWriter, r *http.Request, p authn.Principal)

func (s *Service) wrap(principalFrom principalFunc, h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := principalFrom(r)
		if !ok {
			gcperrors.Unauthenticated(w, "")
			return
		}
		h(w, r, p)
	}
}

func (s *Service) require(p authn.Principal, permission, projectID string) error {
	ok, err := s.Authz.Evaluate(p.Email, p.IsRoot, permission, "projects/"+projectID)
	if err != nil {
		return err
	}
	if !ok {
		return errDenied
	}
	return nil
}

var errDenied = fmt.Errorf("permission denied")

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAuthzErr(w http.ResponseWriter, err error) {
	if err == errDenied {
		gcperrors.PermissionDenied(w, "")
		return
	}
	gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
}

func clusterName(project, location, id string) string {
	return fmt.Sprintf("projects/%s/locations/%s/clusters/%s", project, location, id)
}

func theatreBootstrap(clusterID, location string) string {
	return fmt.Sprintf("%s.%s.kafka.noctaxris-gcp.lab:9092", clusterID, location)
}

func (s *Service) createCluster(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "managedkafka.clusters.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	clusterID := r.URL.Query().Get("clusterId")
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && r.ContentLength != 0 {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if clusterID == "" && body != nil {
		if id, ok := body["name"].(string); ok && id != "" {
			parts := strings.Split(id, "/")
			clusterID = parts[len(parts)-1]
		}
	}
	if clusterID == "" {
		gcperrors.InvalidArgument(w, "clusterId query parameter is required")
		return
	}
	displayName, _ := body["displayName"].(string)
	labelsJSON := "{}"
	if labels, ok := body["labels"]; ok {
		raw, _ := json.Marshal(labels)
		labelsJSON = string(raw)
	}
	name := clusterName(project, location, clusterID)
	bootstrap := theatreBootstrap(clusterID, location)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateKafkaCluster(store.KafkaCluster{
		Name: name, ProjectID: project, Location: location, ClusterID: clusterID,
		DisplayName: displayName, State: "ACTIVE", BootstrapServers: bootstrap,
		LabelsJSON: labelsJSON, CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "cluster already exists")
		return
	}

	s.tryNestedRedpanda(r.Context(), name, clusterID)

	out, ok, err := s.Store.GetKafkaCluster(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created cluster missing")
		return
	}
	writeJSON(w, http.StatusOK, toClusterJSON(out))
}

func (s *Service) tryNestedRedpanda(ctx context.Context, clusterName, clusterID string) {
	cli, err := compute.Dial(s.DockerHost, s.DockerCertPath)
	if err != nil || cli == nil || !cli.Enabled() {
		return
	}
	defer func() { _ = cli.Close() }()

	containerName := compute.RedpandaContainerNameForCluster(clusterID)
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	bootstrap, containerID, err := cli.EnsureRedpanda(runCtx, containerName)
	if err != nil {
		return
	}
	_ = s.Store.UpdateKafkaClusterNested(clusterName, bootstrap, containerID, "ACTIVE")
}

func (s *Service) getCluster(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	clusterID := r.PathValue("cluster")
	if err := s.require(p, "managedkafka.clusters.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	c, ok, err := s.Store.GetKafkaCluster(clusterName(project, location, clusterID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Cluster not found")
		return
	}
	writeJSON(w, http.StatusOK, toClusterJSON(c))
}

func (s *Service) listClusters(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "managedkafka.clusters.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListKafkaClusters(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, c := range list {
		items = append(items, toClusterJSON(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"clusters": items})
}

func (s *Service) deleteCluster(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	clusterID := r.PathValue("cluster")
	if err := s.require(p, "managedkafka.clusters.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := clusterName(project, location, clusterID)
	c, ok, err := s.Store.DeleteKafkaCluster(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Cluster not found")
		return
	}
	s.removeNestedRedpanda(r.Context(), clusterID, c.ContainerID)
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Service) removeNestedRedpanda(ctx context.Context, clusterID, containerID string) {
	cli, err := compute.Dial(s.DockerHost, s.DockerCertPath)
	if err != nil || cli == nil || !cli.Enabled() {
		return
	}
	defer func() { _ = cli.Close() }()
	name := compute.RedpandaContainerNameForCluster(clusterID)
	_ = containerID
	_ = cli.RemoveRedpanda(ctx, name)
}

func toClusterJSON(c store.KafkaCluster) map[string]any {
	var labels any = map[string]string{}
	_ = json.Unmarshal([]byte(c.LabelsJSON), &labels)
	out := map[string]any{
		"name":       c.Name,
		"createTime": c.CreatedAt,
		"state":      c.State,
		"labels":     labels,
	}
	if c.DisplayName != "" {
		out["displayName"] = c.DisplayName
	}
	if c.BootstrapServers != "" {
		out["bootstrapServers"] = c.BootstrapServers
	}
	return out
}
