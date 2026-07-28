package secretmanager

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/iam/apiv1/iampb"
	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// PrincipalFunc extracts the authenticated principal from an HTTP request.
type PrincipalFunc func(r *http.Request) (authn.Principal, bool)

// PrincipalResolver resolves a principal for gRPC.
type PrincipalResolver func(ctx context.Context) (authn.Principal, error)

// Service implements Secret Manager REST + gRPC lab surface.
type Service struct {
	secretmanagerpb.UnimplementedSecretManagerServiceServer

	Store          *store.Store
	Authz          *authz.Evaluator
	HTTPPrincipal  PrincipalFunc
	GRPCPrincipal  PrincipalResolver
	DefaultProject string
}

// RegisterREST mounts Secret Manager v1 REST routes.
func (s *Service) RegisterREST(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/projects/{project}/secrets", s.httpCreateSecret)
	mux.HandleFunc("GET /v1/projects/{project}/secrets", s.httpListSecrets)
	mux.HandleFunc("GET /v1/projects/{project}/secrets/{secret}", s.httpGetSecret)
	mux.HandleFunc("PATCH /v1/projects/{project}/secrets/{secret}", s.httpPatchSecret)
	mux.HandleFunc("DELETE /v1/projects/{project}/secrets/{secret}", s.httpDeleteSecret)
	// Colon custom methods live inside path values (Go ServeMux forbids `{id}:action`).
	mux.HandleFunc("POST /v1/projects/{project}/secrets/{secret}", s.httpSecretPost)
	mux.HandleFunc("GET /v1/projects/{project}/secrets/{secret}/versions", s.httpListVersions)
	mux.HandleFunc("GET /v1/projects/{project}/secrets/{secret}/versions/{version}", s.httpVersionGet)
	mux.HandleFunc("POST /v1/projects/{project}/secrets/{secret}/versions/{version}", s.httpVersionPost)
}

// RegisterGRPC attaches SecretManagerService to gs.
func (s *Service) RegisterGRPC(gs *grpc.Server) {
	secretmanagerpb.RegisterSecretManagerServiceServer(gs, s)
}

func (s *Service) requireHTTP(w http.ResponseWriter, r *http.Request, permission, resource string) bool {
	p, ok := s.HTTPPrincipal(r)
	if !ok {
		gcperrors.Unauthenticated(w, "")
		return false
	}
	allowed, err := s.Authz.Evaluate(p.Email, p.IsRoot, permission, resource)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return false
	}
	if !allowed {
		gcperrors.PermissionDenied(w, "")
		return false
	}
	return true
}

// requireSecretHTTP evaluates permission on the secret resource or the project.
func (s *Service) requireSecretHTTP(w http.ResponseWriter, r *http.Request, permission, secretName, project string) bool {
	p, ok := s.HTTPPrincipal(r)
	if !ok {
		gcperrors.Unauthenticated(w, "")
		return false
	}
	allowed, err := s.Authz.EvaluateAny(p.Email, p.IsRoot, permission, secretName, projectResource(project))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return false
	}
	if !allowed {
		gcperrors.PermissionDenied(w, "")
		return false
	}
	return true
}

func (s *Service) requireGRPC(ctx context.Context, permission, resource string) error {
	if s.GRPCPrincipal == nil {
		return status.Error(codes.Unauthenticated, "gRPC auth resolver not configured")
	}
	p, err := s.GRPCPrincipal(ctx)
	if err != nil {
		return status.Error(codes.Unauthenticated, "unauthenticated")
	}
	allowed, err := s.Authz.Evaluate(p.Email, p.IsRoot, permission, resource)
	if err != nil {
		return status.Errorf(codes.Internal, "%v", err)
	}
	if !allowed {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	return nil
}

func (s *Service) requireSecretGRPC(ctx context.Context, permission, secretName, project string) error {
	if s.GRPCPrincipal == nil {
		return status.Error(codes.Unauthenticated, "gRPC auth resolver not configured")
	}
	p, err := s.GRPCPrincipal(ctx)
	if err != nil {
		return status.Error(codes.Unauthenticated, "unauthenticated")
	}
	allowed, err := s.Authz.EvaluateAny(p.Email, p.IsRoot, permission, secretName, projectResource(project))
	if err != nil {
		return status.Errorf(codes.Internal, "%v", err)
	}
	if !allowed {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	return nil
}

func secretResourceName(project, secretID string) string {
	return "projects/" + project + "/secrets/" + secretID
}

func projectResource(project string) string {
	return "projects/" + project
}

func splitColonAction(v string) (id, action string) {
	if i := strings.IndexByte(v, ':'); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}

func (s *Service) httpCreateSecret(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	if !s.requireHTTP(w, r, "secretmanager.secrets.create", projectResource(project)) {
		return
	}
	secretID := r.URL.Query().Get("secretId")
	if secretID == "" {
		gcperrors.InvalidArgument(w, "secretId query parameter is required")
		return
	}
	var body struct {
		Labels      map[string]string   `json:"labels"`
		Annotations map[string]string   `json:"annotations"`
		Replication map[string]any      `json:"replication"`
		Topics      []map[string]string `json:"topics"`
		Rotation    *struct {
			NextRotationTime string `json:"nextRotationTime"`
			RotationPeriod   string `json:"rotationPeriod"`
		} `json:"rotation"`
		CustomerManagedEncryption *struct {
			KmsKeyName string `json:"kmsKeyName"`
		} `json:"customerManagedEncryption"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	cmek := ""
	if body.CustomerManagedEncryption != nil {
		cmek = body.CustomerManagedEncryption.KmsKeyName
	}
	rotPeriod, nextRot := "", ""
	if body.Rotation != nil {
		rotPeriod = body.Rotation.RotationPeriod
		nextRot = body.Rotation.NextRotationTime
	}
	name := secretResourceName(project, secretID)
	sec, created, err := s.Store.CreateSecretWithRotation(name, project, body.Labels, body.Annotations, body.Replication, cmek, rotPeriod, nextRot, body.Topics)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "secret already exists")
		return
	}
	writeJSON(w, http.StatusOK, secretJSON(sec))
}

func (s *Service) httpListSecrets(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	if !s.requireHTTP(w, r, "secretmanager.secrets.list", projectResource(project)) {
		return
	}
	list, err := s.Store.ListSecrets(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	secrets := make([]map[string]any, 0, len(list))
	for i := range list {
		secrets = append(secrets, secretJSON(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"secrets": secrets})
}

func (s *Service) httpGetSecret(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	secretID, _ := splitColonAction(r.PathValue("secret"))
	name := secretResourceName(project, secretID)
	if !s.requireSecretHTTP(w, r, "secretmanager.secrets.get", name, project) {
		return
	}
	sec, ok, err := s.Store.GetSecret(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "secret not found")
		return
	}
	writeJSON(w, http.StatusOK, secretJSON(sec))
}

func (s *Service) httpPatchSecret(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	secretID, _ := splitColonAction(r.PathValue("secret"))
	name := secretResourceName(project, secretID)
	if !s.requireSecretHTTP(w, r, "secretmanager.secrets.update", name, project) {
		return
	}
	var body struct {
		Labels      *map[string]string   `json:"labels"`
		Annotations *map[string]string   `json:"annotations"`
		Topics      *[]map[string]string `json:"topics"`
		Rotation    *struct {
			NextRotationTime string `json:"nextRotationTime"`
			RotationPeriod   string `json:"rotationPeriod"`
		} `json:"rotation"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid secret patch body")
		return
	}
	var rotPeriod, nextRot *string
	if body.Rotation != nil {
		rotPeriod = &body.Rotation.RotationPeriod
		nextRot = &body.Rotation.NextRotationTime
	}
	sec, err := s.Store.PatchSecretMeta(name, body.Labels, body.Annotations, rotPeriod, nextRot, body.Topics)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			gcperrors.NotFound(w, "secret not found")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, secretJSON(sec))
}

func (s *Service) httpDeleteSecret(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	secretID, _ := splitColonAction(r.PathValue("secret"))
	name := secretResourceName(project, secretID)
	if !s.requireSecretHTTP(w, r, "secretmanager.secrets.delete", name, project) {
		return
	}
	ok, err := s.Store.DeleteSecret(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "secret not found")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func (s *Service) httpSecretPost(w http.ResponseWriter, r *http.Request) {
	_, action := splitColonAction(r.PathValue("secret"))
	switch action {
	case "addVersion":
		s.httpAddVersion(w, r)
	case "rotateSecret":
		s.httpRotateSecret(w, r)
	case "getIamPolicy":
		s.httpGetIamPolicy(w, r)
	case "setIamPolicy":
		s.httpSetIamPolicy(w, r)
	case "testIamPermissions":
		s.httpTestIamPermissions(w, r)
	default:
		gcperrors.InvalidArgument(w, "unsupported secrets custom method")
	}
}

// httpRotateSecret is lab theatre: creates a new SecretVersion (not an official Secret Manager RPC).
// Official Cloud rotation only publishes Pub/Sub; clients add versions themselves.
func (s *Service) httpRotateSecret(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	secretID, _ := splitColonAction(r.PathValue("secret"))
	name := secretResourceName(project, secretID)
	if !s.requireSecretHTTP(w, r, "secretmanager.versions.add", name, project) {
		return
	}
	var body struct {
		Payload *struct {
			Data string `json:"data"`
		} `json:"payload"`
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			gcperrors.InvalidArgument(w, "invalid rotateSecret body")
			return
		}
	}
	var payload []byte
	if body.Payload != nil && body.Payload.Data != "" {
		data, err := base64.StdEncoding.DecodeString(body.Payload.Data)
		if err != nil {
			gcperrors.InvalidArgument(w, "payload.data must be base64")
			return
		}
		payload = data
	}
	v, _, err := s.Store.RotateSecretTheatre(name, payload)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			gcperrors.NotFound(w, "secret not found")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, versionJSON(v))
}

func (s *Service) httpGetIamPolicy(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	secretID, _ := splitColonAction(r.PathValue("secret"))
	name := secretResourceName(project, secretID)
	if !s.requireSecretHTTP(w, r, "secretmanager.secrets.getIamPolicy", name, project) {
		return
	}
	if _, ok, err := s.Store.GetSecret(name); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "secret not found")
		return
	}
	raw, found, err := s.Store.GetIAMPolicyJSON(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, authz.Policy{Etag: "ACAB", Bindings: []authz.Binding{}})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Service) httpSetIamPolicy(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	secretID, _ := splitColonAction(r.PathValue("secret"))
	name := secretResourceName(project, secretID)
	if !s.requireSecretHTTP(w, r, "secretmanager.secrets.setIamPolicy", name, project) {
		return
	}
	if _, ok, err := s.Store.GetSecret(name); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "secret not found")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		Policy authz.Policy `json:"policy"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		gcperrors.InvalidArgument(w, "invalid setIamPolicy body")
		return
	}
	if err := s.Store.PutIAMPolicyJSON(name, req.Policy); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req.Policy)
}

func (s *Service) httpTestIamPermissions(w http.ResponseWriter, r *http.Request) {
	p, ok := s.HTTPPrincipal(r)
	if !ok {
		gcperrors.Unauthenticated(w, "")
		return
	}
	project := r.PathValue("project")
	secretID, _ := splitColonAction(r.PathValue("secret"))
	name := secretResourceName(project, secretID)
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		Permissions []string `json:"permissions"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			gcperrors.InvalidArgument(w, "invalid testIamPermissions body")
			return
		}
	}
	granted, err := s.Authz.TestIamPermissionsAny(p.Email, p.IsRoot, []string{name, projectResource(project)}, req.Permissions)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if granted == nil {
		granted = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": granted})
}

func (s *Service) httpVersionGet(w http.ResponseWriter, r *http.Request) {
	_, action := splitColonAction(r.PathValue("version"))
	switch action {
	case "access":
		s.httpAccessVersion(w, r)
	case "":
		s.httpGetVersion(w, r)
	default:
		gcperrors.InvalidArgument(w, "unsupported secretVersions custom method")
	}
}

func (s *Service) httpVersionPost(w http.ResponseWriter, r *http.Request) {
	_, action := splitColonAction(r.PathValue("version"))
	switch action {
	case "enable":
		s.httpEnableVersion(w, r)
	case "disable":
		s.httpDisableVersion(w, r)
	case "destroy":
		s.httpDestroyVersion(w, r)
	default:
		gcperrors.InvalidArgument(w, "unsupported secretVersions custom method")
	}
}

func (s *Service) httpAddVersion(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	secretID, _ := splitColonAction(r.PathValue("secret"))
	name := secretResourceName(project, secretID)
	if !s.requireSecretHTTP(w, r, "secretmanager.versions.add", name, project) {
		return
	}
	var body struct {
		Payload struct {
			Data string `json:"data"`
		} `json:"payload"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		gcperrors.InvalidArgument(w, "invalid addVersion body")
		return
	}
	data, err := base64.StdEncoding.DecodeString(body.Payload.Data)
	if err != nil {
		gcperrors.InvalidArgument(w, "payload.data must be base64")
		return
	}
	v, err := s.Store.AddSecretVersion(name, data)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			gcperrors.NotFound(w, "secret not found")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, versionJSON(v))
}

func (s *Service) httpAccessVersion(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	secretID, _ := splitColonAction(r.PathValue("secret"))
	name := secretResourceName(project, secretID)
	version, _ := splitColonAction(r.PathValue("version"))
	if !s.requireSecretHTTP(w, r, "secretmanager.versions.access", name, project) {
		return
	}
	plain, v, err := s.Store.AccessSecretVersion(name, version)
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "not found"):
			gcperrors.NotFound(w, "secret version not found")
		case strings.Contains(err.Error(), "destroyed"):
			gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "secret version destroyed")
		case strings.Contains(err.Error(), "disabled"):
			gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "secret version disabled")
		default:
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": v.Name,
		"payload": map[string]any{
			"data": base64.StdEncoding.EncodeToString(plain),
		},
	})
}

func (s *Service) httpEnableVersion(w http.ResponseWriter, r *http.Request) {
	s.httpSetVersionState(w, r, store.SecretVersionEnabled, "secretmanager.versions.enable")
}

func (s *Service) httpDisableVersion(w http.ResponseWriter, r *http.Request) {
	s.httpSetVersionState(w, r, store.SecretVersionDisabled, "secretmanager.versions.disable")
}

func (s *Service) httpDestroyVersion(w http.ResponseWriter, r *http.Request) {
	s.httpSetVersionState(w, r, store.SecretVersionDestroyed, "secretmanager.versions.destroy")
}

func (s *Service) httpSetVersionState(w http.ResponseWriter, r *http.Request, state, permission string) {
	project := r.PathValue("project")
	secretID, _ := splitColonAction(r.PathValue("secret"))
	name := secretResourceName(project, secretID)
	version, _ := splitColonAction(r.PathValue("version"))
	if !s.requireSecretHTTP(w, r, permission, name, project) {
		return
	}
	v, err := s.Store.SetSecretVersionState(name, version, state)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			gcperrors.NotFound(w, "secret version not found")
			return
		}
		if strings.Contains(err.Error(), "destroyed") {
			gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, err.Error())
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, versionJSON(v))
}

func (s *Service) httpListVersions(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	secretID, _ := splitColonAction(r.PathValue("secret"))
	name := secretResourceName(project, secretID)
	if !s.requireSecretHTTP(w, r, "secretmanager.versions.list", name, project) {
		return
	}
	list, err := s.Store.ListSecretVersions(name, parseSecretVersionStateFilter(r.URL.Query().Get("filter")))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	versions := make([]map[string]any, 0, len(list))
	for i := range list {
		versions = append(versions, versionJSON(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

func (s *Service) httpGetVersion(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	secretID, _ := splitColonAction(r.PathValue("secret"))
	name := secretResourceName(project, secretID)
	version, _ := splitColonAction(r.PathValue("version"))
	if !s.requireSecretHTTP(w, r, "secretmanager.versions.get", name, project) {
		return
	}
	v, ok, err := s.Store.GetSecretVersion(name, version)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "secret version not found")
		return
	}
	writeJSON(w, http.StatusOK, versionJSON(v))
}

func secretJSON(sec *store.Secret) map[string]any {
	labels := sec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	annotations := sec.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	replication := sec.Replication
	if replication == nil || len(replication) == 0 {
		replication = map[string]any{"automatic": map[string]any{}}
	}
	out := map[string]any{
		"name":        sec.Name,
		"createTime":  sec.CreatedAt,
		"labels":      labels,
		"annotations": annotations,
		"replication": replication,
	}
	if sec.CMEKKmsKeyName != "" {
		out["customerManagedEncryption"] = map[string]any{"kmsKeyName": sec.CMEKKmsKeyName}
	}
	if sec.RotationPeriod != "" || sec.NextRotationTime != "" {
		rot := map[string]any{}
		if sec.NextRotationTime != "" {
			rot["nextRotationTime"] = sec.NextRotationTime
		}
		if sec.RotationPeriod != "" {
			rot["rotationPeriod"] = sec.RotationPeriod
		}
		out["rotation"] = rot
	}
	if sec.TopicsJSON != "" && sec.TopicsJSON != "[]" {
		var topics []map[string]string
		if err := json.Unmarshal([]byte(sec.TopicsJSON), &topics); err == nil {
			out["topics"] = topics
		}
	}
	return out
}

func parseSecretVersionStateFilter(filter string) string {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return ""
	}
	// Lab subset: state:ENABLED | state="ENABLED" | state:DISABLED | ...
	filter = strings.ReplaceAll(filter, `"`, "")
	lower := strings.ToLower(filter)
	switch {
	case strings.HasPrefix(lower, "state:"):
		return strings.ToUpper(strings.TrimSpace(filter[len("state:"):]))
	case strings.HasPrefix(lower, "state="):
		return strings.ToUpper(strings.TrimSpace(filter[len("state="):]))
	default:
		return strings.ToUpper(filter)
	}
}

func versionJSON(v *store.SecretVersion) map[string]any {
	return map[string]any{
		"name":       v.Name,
		"createTime": v.CreatedAt,
		"state":      v.State,
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// --- gRPC ---

func (s *Service) CreateSecret(ctx context.Context, req *secretmanagerpb.CreateSecretRequest) (*secretmanagerpb.Secret, error) {
	project := strings.TrimPrefix(req.GetParent(), "projects/")
	if project == "" || strings.Contains(project, "/") {
		return nil, status.Error(codes.InvalidArgument, "invalid parent")
	}
	if err := s.requireGRPC(ctx, "secretmanager.secrets.create", projectResource(project)); err != nil {
		return nil, err
	}
	name := secretResourceName(project, req.GetSecretId())
	var replication map[string]any
	cmek := ""
	var labels, annotations map[string]string
	if req.GetSecret() != nil {
		labels = req.GetSecret().GetLabels()
		annotations = req.GetSecret().GetAnnotations()
		if req.GetSecret().GetReplication() != nil {
			raw, _ := json.Marshal(req.GetSecret().GetReplication())
			_ = json.Unmarshal(raw, &replication)
		}
		if req.GetSecret().GetCustomerManagedEncryption() != nil {
			cmek = req.GetSecret().GetCustomerManagedEncryption().GetKmsKeyName()
		}
	}
	sec, created, err := s.Store.CreateSecretWithMeta(name, project, labels, annotations, replication, cmek)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !created {
		return nil, status.Error(codes.AlreadyExists, "secret already exists")
	}
	return secretPB(sec), nil
}

func (s *Service) GetSecret(ctx context.Context, req *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
	project := projectFromSecretName(req.GetName())
	if err := s.requireSecretGRPC(ctx, "secretmanager.secrets.get", req.GetName(), project); err != nil {
		return nil, err
	}
	sec, ok, err := s.Store.GetSecret(req.GetName())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "secret not found")
	}
	return secretPB(sec), nil
}

func (s *Service) UpdateSecret(ctx context.Context, req *secretmanagerpb.UpdateSecretRequest) (*secretmanagerpb.Secret, error) {
	name := req.GetSecret().GetName()
	project := projectFromSecretName(name)
	if err := s.requireSecretGRPC(ctx, "secretmanager.secrets.update", name, project); err != nil {
		return nil, err
	}
	paths := req.GetUpdateMask().GetPaths()
	if len(paths) == 0 {
		return nil, status.Error(codes.InvalidArgument, "update_mask required")
	}
	var labels, annotations *map[string]string
	for _, p := range paths {
		switch p {
		case "labels":
			l := req.GetSecret().GetLabels()
			if l == nil {
				l = map[string]string{}
			}
			labels = &l
		case "annotations":
			a := req.GetSecret().GetAnnotations()
			if a == nil {
				a = map[string]string{}
			}
			annotations = &a
		}
	}
	sec, err := s.Store.PatchSecret(name, labels, annotations)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, status.Error(codes.NotFound, "secret not found")
		}
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return secretPB(sec), nil
}

func (s *Service) ListSecrets(ctx context.Context, req *secretmanagerpb.ListSecretsRequest) (*secretmanagerpb.ListSecretsResponse, error) {
	project := strings.TrimPrefix(req.GetParent(), "projects/")
	if project == "" || strings.Contains(project, "/") {
		return nil, status.Error(codes.InvalidArgument, "invalid parent")
	}
	if err := s.requireGRPC(ctx, "secretmanager.secrets.list", projectResource(project)); err != nil {
		return nil, err
	}
	list, err := s.Store.ListSecrets(project)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	out := &secretmanagerpb.ListSecretsResponse{}
	for i := range list {
		out.Secrets = append(out.Secrets, secretPB(&list[i]))
	}
	return out, nil
}

func (s *Service) DeleteSecret(ctx context.Context, req *secretmanagerpb.DeleteSecretRequest) (*emptypb.Empty, error) {
	project := projectFromSecretName(req.GetName())
	if err := s.requireSecretGRPC(ctx, "secretmanager.secrets.delete", req.GetName(), project); err != nil {
		return nil, err
	}
	ok, err := s.Store.DeleteSecret(req.GetName())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "secret not found")
	}
	return &emptypb.Empty{}, nil
}

func (s *Service) AddSecretVersion(ctx context.Context, req *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
	project := projectFromSecretName(req.GetParent())
	if err := s.requireSecretGRPC(ctx, "secretmanager.versions.add", req.GetParent(), project); err != nil {
		return nil, err
	}
	payload := req.GetPayload().GetData()
	v, err := s.Store.AddSecretVersion(req.GetParent(), payload)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, status.Error(codes.NotFound, "secret not found")
		}
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return versionPB(v), nil
}

func (s *Service) AccessSecretVersion(ctx context.Context, req *secretmanagerpb.AccessSecretVersionRequest) (*secretmanagerpb.AccessSecretVersionResponse, error) {
	secretName, versionID, ok := store.ParseSecretVersionName(req.GetName())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "invalid version name")
	}
	project := projectFromSecretName(secretName)
	if err := s.requireSecretGRPC(ctx, "secretmanager.versions.access", secretName, project); err != nil {
		return nil, err
	}
	plain, v, err := s.Store.AccessSecretVersion(secretName, versionID)
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "not found"):
			return nil, status.Error(codes.NotFound, "secret version not found")
		case strings.Contains(err.Error(), "destroyed"), strings.Contains(err.Error(), "disabled"):
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		default:
			return nil, status.Errorf(codes.Internal, "%v", err)
		}
	}
	return &secretmanagerpb.AccessSecretVersionResponse{
		Name:    v.Name,
		Payload: &secretmanagerpb.SecretPayload{Data: plain},
	}, nil
}

func (s *Service) EnableSecretVersion(ctx context.Context, req *secretmanagerpb.EnableSecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
	return s.grpcSetState(ctx, req.GetName(), store.SecretVersionEnabled, "secretmanager.versions.enable")
}

func (s *Service) DisableSecretVersion(ctx context.Context, req *secretmanagerpb.DisableSecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
	return s.grpcSetState(ctx, req.GetName(), store.SecretVersionDisabled, "secretmanager.versions.disable")
}

func (s *Service) DestroySecretVersion(ctx context.Context, req *secretmanagerpb.DestroySecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
	return s.grpcSetState(ctx, req.GetName(), store.SecretVersionDestroyed, "secretmanager.versions.destroy")
}

func (s *Service) grpcSetState(ctx context.Context, name, state, permission string) (*secretmanagerpb.SecretVersion, error) {
	secretName, versionID, ok := store.ParseSecretVersionName(name)
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "invalid version name")
	}
	project := projectFromSecretName(secretName)
	if err := s.requireSecretGRPC(ctx, permission, secretName, project); err != nil {
		return nil, err
	}
	v, err := s.Store.SetSecretVersionState(secretName, versionID, state)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, status.Error(codes.NotFound, "secret version not found")
		}
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return versionPB(v), nil
}

func (s *Service) GetIamPolicy(ctx context.Context, req *iampb.GetIamPolicyRequest) (*iampb.Policy, error) {
	project := projectFromSecretName(req.GetResource())
	if err := s.requireSecretGRPC(ctx, "secretmanager.secrets.getIamPolicy", req.GetResource(), project); err != nil {
		return nil, err
	}
	raw, ok, err := s.Store.GetIAMPolicyJSON(req.GetResource())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return &iampb.Policy{Etag: []byte("ACAB")}, nil
	}
	return authzToIAMPolicy(raw)
}

func (s *Service) SetIamPolicy(ctx context.Context, req *iampb.SetIamPolicyRequest) (*iampb.Policy, error) {
	project := projectFromSecretName(req.GetResource())
	if err := s.requireSecretGRPC(ctx, "secretmanager.secrets.setIamPolicy", req.GetResource(), project); err != nil {
		return nil, err
	}
	pol := iamPolicyToAuthz(req.GetPolicy())
	if err := s.Store.PutIAMPolicyJSON(req.GetResource(), pol); err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return req.GetPolicy(), nil
}

func (s *Service) TestIamPermissions(ctx context.Context, req *iampb.TestIamPermissionsRequest) (*iampb.TestIamPermissionsResponse, error) {
	if s.GRPCPrincipal == nil {
		return nil, status.Error(codes.Unauthenticated, "gRPC auth resolver not configured")
	}
	p, err := s.GRPCPrincipal(ctx)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	project := projectFromSecretName(req.GetResource())
	granted, err := s.Authz.TestIamPermissionsAny(p.Email, p.IsRoot, []string{req.GetResource(), projectResource(project)}, req.GetPermissions())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return &iampb.TestIamPermissionsResponse{Permissions: granted}, nil
}

func authzToIAMPolicy(raw []byte) (*iampb.Policy, error) {
	var pol authz.Policy
	if err := json.Unmarshal(raw, &pol); err != nil {
		return nil, status.Errorf(codes.Internal, "parse policy: %v", err)
	}
	out := &iampb.Policy{Etag: []byte(pol.Etag)}
	for _, b := range pol.Bindings {
		out.Bindings = append(out.Bindings, &iampb.Binding{Role: b.Role, Members: b.Members})
	}
	return out, nil
}

func iamPolicyToAuthz(p *iampb.Policy) authz.Policy {
	out := authz.Policy{Etag: string(p.GetEtag())}
	for _, b := range p.GetBindings() {
		out.Bindings = append(out.Bindings, authz.Binding{Role: b.GetRole(), Members: b.GetMembers()})
	}
	return out
}

func projectFromSecretName(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) >= 2 && parts[0] == "projects" {
		return parts[1]
	}
	return ""
}

func secretPB(sec *store.Secret) *secretmanagerpb.Secret {
	out := &secretmanagerpb.Secret{
		Name:        sec.Name,
		Labels:      sec.Labels,
		Annotations: sec.Annotations,
	}
	if t, err := parseTime(sec.CreatedAt); err == nil {
		out.CreateTime = timestamppb.New(t)
	}
	if sec.CMEKKmsKeyName != "" {
		out.CustomerManagedEncryption = &secretmanagerpb.CustomerManagedEncryption{KmsKeyName: sec.CMEKKmsKeyName}
	}
	if len(sec.Replication) > 0 {
		if _, ok := sec.Replication["automatic"]; ok {
			out.Replication = &secretmanagerpb.Replication{
				Replication: &secretmanagerpb.Replication_Automatic_{
					Automatic: &secretmanagerpb.Replication_Automatic{},
				},
			}
		} else if um, ok := sec.Replication["userManaged"].(map[string]any); ok {
			userManaged := &secretmanagerpb.Replication_UserManaged{}
			if reps, ok := um["replicas"].([]any); ok {
				for _, r := range reps {
					rm, _ := r.(map[string]any)
					loc, _ := rm["location"].(string)
					if loc != "" {
						userManaged.Replicas = append(userManaged.Replicas, &secretmanagerpb.Replication_UserManaged_Replica{Location: loc})
					}
				}
			}
			out.Replication = &secretmanagerpb.Replication{
				Replication: &secretmanagerpb.Replication_UserManaged_{UserManaged: userManaged},
			}
		}
	}
	if out.Replication == nil {
		out.Replication = &secretmanagerpb.Replication{
			Replication: &secretmanagerpb.Replication_Automatic_{
				Automatic: &secretmanagerpb.Replication_Automatic{},
			},
		}
	}
	return out
}

func (s *Service) ListSecretVersions(ctx context.Context, req *secretmanagerpb.ListSecretVersionsRequest) (*secretmanagerpb.ListSecretVersionsResponse, error) {
	project := projectFromSecretName(req.GetParent())
	if err := s.requireSecretGRPC(ctx, "secretmanager.versions.list", req.GetParent(), project); err != nil {
		return nil, err
	}
	list, err := s.Store.ListSecretVersions(req.GetParent(), parseSecretVersionStateFilter(req.GetFilter()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	out := &secretmanagerpb.ListSecretVersionsResponse{}
	for i := range list {
		out.Versions = append(out.Versions, versionPB(&list[i]))
	}
	return out, nil
}

func versionPB(v *store.SecretVersion) *secretmanagerpb.SecretVersion {
	out := &secretmanagerpb.SecretVersion{Name: v.Name}
	switch v.State {
	case store.SecretVersionEnabled:
		out.State = secretmanagerpb.SecretVersion_ENABLED
	case store.SecretVersionDisabled:
		out.State = secretmanagerpb.SecretVersion_DISABLED
	case store.SecretVersionDestroyed:
		out.State = secretmanagerpb.SecretVersion_DESTROYED
	}
	if t, err := parseTime(v.CreatedAt); err == nil {
		out.CreateTime = timestamppb.New(t)
	}
	return out
}

func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}
