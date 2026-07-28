package secretmanager

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

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
	name := secretResourceName(project, secretID)
	sec, created, err := s.Store.CreateSecret(name, project)
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
	if !s.requireHTTP(w, r, "secretmanager.secrets.get", projectResource(project)) {
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

func (s *Service) httpDeleteSecret(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	secretID, _ := splitColonAction(r.PathValue("secret"))
	name := secretResourceName(project, secretID)
	if !s.requireHTTP(w, r, "secretmanager.secrets.delete", projectResource(project)) {
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
	default:
		gcperrors.InvalidArgument(w, "unsupported secrets custom method")
	}
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
	if !s.requireHTTP(w, r, "secretmanager.versions.add", projectResource(project)) {
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
	if !s.requireHTTP(w, r, "secretmanager.versions.access", projectResource(project)) {
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
	if !s.requireHTTP(w, r, permission, projectResource(project)) {
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
	if !s.requireHTTP(w, r, "secretmanager.versions.list", projectResource(project)) {
		return
	}
	list, err := s.Store.ListSecretVersions(name)
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
	if !s.requireHTTP(w, r, "secretmanager.versions.get", projectResource(project)) {
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
	return map[string]any{
		"name":       sec.Name,
		"createTime": sec.CreatedAt,
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
	sec, created, err := s.Store.CreateSecret(name, project)
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
	if err := s.requireGRPC(ctx, "secretmanager.secrets.get", projectResource(project)); err != nil {
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
	if err := s.requireGRPC(ctx, "secretmanager.secrets.delete", projectResource(project)); err != nil {
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
	if err := s.requireGRPC(ctx, "secretmanager.versions.add", projectResource(project)); err != nil {
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
	if err := s.requireGRPC(ctx, "secretmanager.versions.access", projectResource(project)); err != nil {
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
	if err := s.requireGRPC(ctx, permission, projectResource(project)); err != nil {
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

func projectFromSecretName(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) >= 2 && parts[0] == "projects" {
		return parts[1]
	}
	return ""
}

func secretPB(sec *store.Secret) *secretmanagerpb.Secret {
	out := &secretmanagerpb.Secret{Name: sec.Name}
	if t, err := parseTime(sec.CreatedAt); err == nil {
		out.CreateTime = timestamppb.New(t)
	}
	return out
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
