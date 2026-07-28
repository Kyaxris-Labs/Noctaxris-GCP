package certificatemanager

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// DefaultLocation is the lab default Certificate Manager location (global OK).
const DefaultLocation = "global"

// Service serves Certificate Manager REST v1 (certificates + certificateMaps).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Certificate Manager v1 REST routes.
// Colon methods on resource segments are parsed via splitColonAction (none required for this cut).
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/certificates", s.wrap(principalFrom, s.listCertificates))
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/certificates", s.wrap(principalFrom, s.createCertificate))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/certificates/{certificate}", s.wrap(principalFrom, s.getCertificate))
	mux.HandleFunc("DELETE /v1/projects/{project}/locations/{location}/certificates/{certificate}", s.wrap(principalFrom, s.deleteCertificate))

	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/certificateMaps", s.wrap(principalFrom, s.listCertificateMaps))
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/certificateMaps", s.wrap(principalFrom, s.createCertificateMap))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/certificateMaps/{certificateMap}", s.wrap(principalFrom, s.getCertificateMap))
	mux.HandleFunc("DELETE /v1/projects/{project}/locations/{location}/certificateMaps/{certificateMap}", s.wrap(principalFrom, s.deleteCertificateMap))

	// Lab Operations.get: create returns done:true; poll path succeeds immediately for TF waiters.
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/operations/{operation}", s.wrap(principalFrom, s.getOperation))
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

func splitColonAction(seg string) (name, action string) {
	if i := strings.IndexByte(seg, ':'); i >= 0 {
		return seg[:i], seg[i+1:]
	}
	return seg, ""
}

func decodeBody(r *http.Request) (map[string]any, error) {
	defer r.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]any{}, nil
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	if body == nil {
		body = map[string]any{}
	}
	return body, nil
}

func labelsJSON(body map[string]any) string {
	if labels, ok := body["labels"]; ok {
		raw, _ := json.Marshal(labels)
		return string(raw)
	}
	return "{}"
}

func (s *Service) createCertificate(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "certificatemanager.certs.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	certID := r.URL.Query().Get("certificateId")
	if certID == "" {
		if n, _ := body["name"].(string); n != "" {
			parts := strings.Split(n, "/")
			certID = parts[len(parts)-1]
		}
	}
	certID = strings.TrimSpace(certID)
	if certID == "" {
		gcperrors.InvalidArgument(w, "certificateId is required")
		return
	}
	desc, _ := body["description"].(string)
	scope, _ := body["scope"].(string)
	if scope == "" {
		scope = "DEFAULT"
	}
	certType := "SELF_MANAGED"
	if _, ok := body["managed"]; ok {
		certType = "MANAGED"
	}
	if _, ok := body["selfManaged"]; ok {
		certType = "SELF_MANAGED"
	}
	extras := map[string]any{}
	for k, v := range body {
		switch k {
		case "name", "description", "labels", "scope", "createTime", "updateTime":
			continue
		default:
			extras[k] = v
		}
	}
	// Do not persist private key material beyond theatre metadata flags.
	if sm, ok := extras["selfManaged"].(map[string]any); ok {
		sanitized := map[string]any{}
		if _, has := sm["pemCertificate"]; has {
			sanitized["pemCertificate"] = "(redacted lab theatre)"
		}
		if _, has := sm["pemPrivateKey"]; has {
			sanitized["pemPrivateKeyPresent"] = true
		}
		extras["selfManaged"] = sanitized
	}
	if managed, ok := extras["managed"].(map[string]any); ok {
		managed["state"] = "ACTIVE"
		extras["managed"] = managed
	}
	extrasJSON, _ := json.Marshal(extras)
	name := store.CertManagerCertificateResourceName(project, location, certID)
	created, err := s.Store.CreateCertManagerCertificate(store.CertManagerCertificate{
		Name: name, ProjectID: project, Location: location, CertificateID: certID,
		Description: desc, LabelsJSON: labelsJSON(body), Scope: scope,
		CertType: certType, BodyJSON: string(extrasJSON),
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "certificate already exists")
		return
	}
	out, ok, err := s.Store.GetCertManagerCertificate(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created certificate missing")
		return
	}
	writeDoneOperation(w, project, location, "create-"+certID, withType(toCertificateJSON(out),
		"type.googleapis.com/google.cloud.certificatemanager.v1.Certificate"))
}

func (s *Service) getCertificate(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, action := splitColonAction(r.PathValue("certificate"))
	if action != "" {
		gcperrors.NotFound(w, "unknown Certificate Manager method")
		return
	}
	if err := s.require(p, "certificatemanager.certs.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := store.CertManagerCertificateResourceName(project, location, id)
	c, ok, err := s.Store.GetCertManagerCertificate(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Certificate not found")
		return
	}
	writeJSON(w, http.StatusOK, toCertificateJSON(c))
}

func (s *Service) listCertificates(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "certificatemanager.certs.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListCertManagerCertificates(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, c := range list {
		items = append(items, toCertificateJSON(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"certificates": items})
}

func (s *Service) deleteCertificate(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitColonAction(r.PathValue("certificate"))
	if err := s.require(p, "certificatemanager.certs.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := store.CertManagerCertificateResourceName(project, location, id)
	ok, err := s.Store.DeleteCertManagerCertificate(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Certificate not found")
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{})
}

func (s *Service) createCertificateMap(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "certificatemanager.certmaps.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	mapID := r.URL.Query().Get("certificateMapId")
	if mapID == "" {
		if n, _ := body["name"].(string); n != "" {
			parts := strings.Split(n, "/")
			mapID = parts[len(parts)-1]
		}
	}
	mapID = strings.TrimSpace(mapID)
	if mapID == "" {
		gcperrors.InvalidArgument(w, "certificateMapId is required")
		return
	}
	desc, _ := body["description"].(string)
	extras := map[string]any{}
	for k, v := range body {
		switch k {
		case "name", "description", "labels", "createTime", "updateTime", "gclbTargets":
			continue
		default:
			extras[k] = v
		}
	}
	extrasJSON, _ := json.Marshal(extras)
	name := store.CertManagerCertificateMapResourceName(project, location, mapID)
	created, err := s.Store.CreateCertManagerCertificateMap(store.CertManagerCertificateMap{
		Name: name, ProjectID: project, Location: location, MapID: mapID,
		Description: desc, LabelsJSON: labelsJSON(body), BodyJSON: string(extrasJSON),
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "certificate map already exists")
		return
	}
	out, ok, err := s.Store.GetCertManagerCertificateMap(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created certificate map missing")
		return
	}
	writeDoneOperation(w, project, location, "create-"+mapID, withType(toCertificateMapJSON(out),
		"type.googleapis.com/google.cloud.certificatemanager.v1.CertificateMap"))
}

func (s *Service) getOperation(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	opID, _ := splitColonAction(r.PathValue("operation"))
	if err := s.require(p, "certificatemanager.operations.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	opName := fmt.Sprintf("projects/%s/locations/%s/operations/%s", project, location, opID)
	writeJSON(w, http.StatusOK, map[string]any{
		"name": opName,
		"done": true,
	})
}

func (s *Service) getCertificateMap(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, action := splitColonAction(r.PathValue("certificateMap"))
	if action != "" {
		gcperrors.NotFound(w, "unknown Certificate Manager method")
		return
	}
	if err := s.require(p, "certificatemanager.certmaps.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := store.CertManagerCertificateMapResourceName(project, location, id)
	m, ok, err := s.Store.GetCertManagerCertificateMap(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "CertificateMap not found")
		return
	}
	writeJSON(w, http.StatusOK, toCertificateMapJSON(m))
}

func (s *Service) listCertificateMaps(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "certificatemanager.certmaps.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListCertManagerCertificateMaps(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, m := range list {
		items = append(items, toCertificateMapJSON(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"certificateMaps": items})
}

func (s *Service) deleteCertificateMap(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitColonAction(r.PathValue("certificateMap"))
	if err := s.require(p, "certificatemanager.certmaps.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := store.CertManagerCertificateMapResourceName(project, location, id)
	ok, err := s.Store.DeleteCertManagerCertificateMap(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "CertificateMap not found")
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{})
}

// writeDoneOperation returns a completed LRO so Terraform
// CertificateManagerOperationWaitTime does not treat the resource name as an unfinished op.
func writeDoneOperation(w http.ResponseWriter, project, location, opID string, response any) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     fmt.Sprintf("projects/%s/locations/%s/operations/%s", project, location, opID),
		"done":     true,
		"response": response,
	})
}

func withType(m map[string]any, typeURL string) map[string]any {
	m["@type"] = typeURL
	return m
}

func toCertificateJSON(c store.CertManagerCertificate) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal([]byte(c.BodyJSON), &out)
	if out == nil {
		out = map[string]any{}
	}
	labels := map[string]string{}
	_ = json.Unmarshal([]byte(c.LabelsJSON), &labels)
	out["name"] = c.Name
	out["description"] = c.Description
	out["labels"] = labels
	out["scope"] = c.Scope
	out["createTime"] = c.CreatedAt
	out["updateTime"] = c.UpdatedAt
	if c.CertType == "MANAGED" {
		if _, ok := out["managed"]; !ok {
			out["managed"] = map[string]any{"state": "ACTIVE"}
		}
	}
	return out
}

func toCertificateMapJSON(m store.CertManagerCertificateMap) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal([]byte(m.BodyJSON), &out)
	if out == nil {
		out = map[string]any{}
	}
	labels := map[string]string{}
	_ = json.Unmarshal([]byte(m.LabelsJSON), &labels)
	out["name"] = m.Name
	out["description"] = m.Description
	out["labels"] = labels
	out["createTime"] = m.CreatedAt
	out["updateTime"] = m.UpdatedAt
	if _, ok := out["gclbTargets"]; !ok {
		out["gclbTargets"] = []any{}
	}
	return out
}
