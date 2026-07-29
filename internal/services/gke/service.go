package gke

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// DefaultLocation is the lab default GKE region.
const DefaultLocation = "us-central1"

// K3sLabImage is the pinned nested k3s image (no host port publish).
const K3sLabImage = "rancher/k3s:v1.28.8-k3s1"

// Service serves Container API v1 cluster routes.
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers GKE cluster REST routes (Container API; /container/v1 prefix).
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /container/v1/projects/{project}/locations/{location}/clusters", s.wrap(principalFrom, s.listClusters))
	mux.HandleFunc("POST /container/v1/projects/{project}/locations/{location}/clusters", s.wrap(principalFrom, s.createCluster))
	mux.HandleFunc("GET /container/v1/projects/{project}/locations/{location}/clusters/{cluster}", s.wrap(principalFrom, s.getCluster))
	mux.HandleFunc("DELETE /container/v1/projects/{project}/locations/{location}/clusters/{cluster}", s.wrap(principalFrom, s.deleteCluster))
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

var errDenied = fmt.Errorf("permission denied")

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

func theatreEndpoint(clusterID string) string {
	return fmt.Sprintf("https://%s.gke.goog", clusterID)
}

func (s *Service) createCluster(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "container.clusters.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if !restlab.RequireServiceEnabled(w, s.Store, project, "container.googleapis.com") {
		return
	}
	clusterID := r.URL.Query().Get("clusterId")
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if clusterID == "" {
		if name, ok := body["name"].(string); ok && name != "" {
			parts := strings.Split(name, "/")
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
	endpoint := theatreEndpoint(clusterID)
	nestedJSON := "{}"
	if detail := tryNestedK3s(r.Context()); detail != nil {
		raw, _ := json.Marshal(detail)
		nestedJSON = string(raw)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateGKECluster(store.GKECluster{
		Name: name, ProjectID: project, Location: location, ClusterID: clusterID,
		DisplayName: displayName, Status: "RUNNING", Endpoint: endpoint,
		LabelsJSON: labelsJSON, NestedDetailJSON: nestedJSON, CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "cluster already exists")
		return
	}
	out, ok, err := s.Store.GetGKECluster(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created cluster missing")
		return
	}
	writeJSON(w, http.StatusOK, toClusterJSON(out))
}

func tryNestedK3s(ctx context.Context) map[string]any {
	host := strings.TrimSpace(os.Getenv(compute.EnvDockerHost))
	if host == "" {
		return nil
	}
	if err := compute.AllowImagePull(K3sLabImage); err != nil {
		return map[string]any{"mode": "skipped", "detail": err.Error()}
	}
	cli, err := compute.Dial(host, os.Getenv(compute.EnvDockerCertPath))
	if err != nil || !cli.Enabled() {
		return map[string]any{"mode": "mock", "detail": "engine unavailable"}
	}
	defer cli.Close()
	out, err := cli.RunLabOneShot(ctx, K3sLabImage)
	if err != nil {
		return map[string]any{"mode": "mock", "detail": "k3s one-shot failed"}
	}
	return map[string]any{
		"mode":     "nested-one-shot",
		"image":    out.Image,
		"exitCode": out.ExitCode,
		"stdout":   out.Stdout,
	}
}

func (s *Service) getCluster(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	clusterID := r.PathValue("cluster")
	if err := s.require(p, "container.clusters.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := clusterName(project, location, clusterID)
	c, ok, err := s.Store.GetGKECluster(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "cluster not found")
		return
	}
	writeJSON(w, http.StatusOK, toClusterJSON(c))
}

func (s *Service) listClusters(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "container.clusters.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	rows, err := s.Store.ListGKEClusters(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]any, 0, len(rows))
	for _, c := range rows {
		items = append(items, toClusterJSON(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"clusters": items})
}

func (s *Service) deleteCluster(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	clusterID := r.PathValue("cluster")
	if err := s.require(p, "container.clusters.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := clusterName(project, location, clusterID)
	ok, err := s.Store.DeleteGKECluster(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "cluster not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func toClusterJSON(c store.GKECluster) map[string]any {
	out := map[string]any{
		"name":                 c.Name,
		"location":             c.Location,
		"status":               c.Status,
		"endpoint":             c.Endpoint,
		"currentMasterVersion": c.MasterVersion,
		"createTime":           c.CreatedAt,
	}
	if c.DisplayName != "" {
		out["displayName"] = c.DisplayName
	}
	if c.LabelsJSON != "" && c.LabelsJSON != "{}" {
		var labels map[string]any
		if json.Unmarshal([]byte(c.LabelsJSON), &labels) == nil {
			out["resourceLabels"] = labels
		}
	}
	if c.NestedDetailJSON != "" && c.NestedDetailJSON != "{}" {
		var nested map[string]any
		if json.Unmarshal([]byte(c.NestedDetailJSON), &nested) == nil {
			out["noctaxrisNestedEngine"] = nested
		}
	}
	return out
}
