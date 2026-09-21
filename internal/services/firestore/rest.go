package firestore

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type restPrincipalFunc func(*http.Request) (authn.Principal, bool)

// MountREST registers Firestore REST document create/patch on the shared HTTP mux.
// Owner-write ACL matches gRPC: Identity Toolkit uid may write only
// .../documents/users/{uid}. Database id is (default) only.
func (s *Service) MountREST(mux *http.ServeMux, principalFrom restPrincipalFunc) {
	mux.HandleFunc("POST /v1/projects/{project}/databases/{database}/documents/{collection}", s.wrapREST(principalFrom, s.restCreateDocument))
	mux.HandleFunc("PATCH /v1/projects/{project}/databases/{database}/documents/{document...}", s.wrapREST(principalFrom, s.restPatchDocument))
}

func (s *Service) wrapREST(principalFrom restPrincipalFunc, h func(http.ResponseWriter, *http.Request, authn.Principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := principalFrom(r)
		if !ok {
			gcperrors.Unauthenticated(w, "")
			return
		}
		h(w, r, p)
	}
}

func (s *Service) restCreateDocument(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	database := r.PathValue("database")
	collection := r.PathValue("collection")
	if err := requireDefaultDatabase(database); err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	if collection == "" || strings.Contains(collection, "/") {
		gcperrors.InvalidArgument(w, "collection is required")
		return
	}
	docID := r.URL.Query().Get("documentId")
	if docID == "" {
		docID = newDocID()
	}
	path := fmt.Sprintf("projects/%s/databases/(default)/documents/%s/%s", project, collection, docID)
	if err := s.authorizeWritePrincipal(p, "datastore.entities.create", project, path); err != nil {
		writeFirestoreRESTErr(w, err)
		return
	}
	fieldsJSON, err := restFieldsJSON(r)
	if err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	if _, ok, err := s.Store.GetFirestoreDoc(path); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if ok {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "Document already exists: "+path)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	d := store.FirestoreDoc{
		Path: path, ProjectID: project, CollectionID: collection, DocumentID: docID,
		FieldsJSON: fieldsJSON, CreateTime: now, UpdateTime: now,
	}
	if err := s.Store.PutFirestoreDoc(d); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSONREST(w, http.StatusOK, restDocument(d))
}

func (s *Service) restPatchDocument(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	database := r.PathValue("database")
	docPath := strings.Trim(r.PathValue("document"), "/")
	if err := requireDefaultDatabase(database); err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	if docPath == "" || !strings.Contains(docPath, "/") {
		gcperrors.InvalidArgument(w, "document path is required")
		return
	}
	path := fmt.Sprintf("projects/%s/databases/(default)/documents/%s", project, docPath)
	if err := s.authorizeWritePrincipal(p, "datastore.entities.update", project, path); err != nil {
		writeFirestoreRESTErr(w, err)
		return
	}
	fieldsJSON, err := restFieldsJSON(r)
	if err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	existing, ok, err := s.Store.GetFirestoreDoc(path)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	parts := strings.Split(docPath, "/")
	coll, docID := parts[0], parts[len(parts)-1]
	d := store.FirestoreDoc{
		Path: path, ProjectID: project, CollectionID: coll, DocumentID: docID,
		FieldsJSON: fieldsJSON, CreateTime: now, UpdateTime: now,
	}
	if ok {
		d.CreateTime = existing.CreateTime
	}
	if err := s.Store.PutFirestoreDoc(d); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSONREST(w, http.StatusOK, restDocument(d))
}

func requireDefaultDatabase(database string) error {
	if database != "(default)" {
		return fmt.Errorf("only database (default) is supported")
	}
	return nil
}

func restFieldsJSON(r *http.Request) (string, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("unable to read body")
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return "{}", nil
	}
	var wrap struct {
		Fields json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return "", fmt.Errorf("invalid JSON body")
	}
	if len(wrap.Fields) == 0 {
		return "{}", nil
	}
	return string(wrap.Fields), nil
}

func restDocument(d store.FirestoreDoc) map[string]any {
	var fields any = map[string]any{}
	if d.FieldsJSON != "" && d.FieldsJSON != "{}" {
		_ = json.Unmarshal([]byte(d.FieldsJSON), &fields)
	}
	return map[string]any{
		"name":       d.Path,
		"fields":     fields,
		"createTime": d.CreateTime,
		"updateTime": d.UpdateTime,
	}
}

func writeJSONREST(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeFirestoreRESTErr(w http.ResponseWriter, err error) {
	st, ok := status.FromError(err)
	if !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	switch st.Code() {
	case codes.PermissionDenied:
		gcperrors.PermissionDenied(w, st.Message())
	case codes.Unauthenticated:
		gcperrors.Unauthenticated(w, st.Message())
	case codes.NotFound:
		gcperrors.NotFound(w, st.Message())
	case codes.AlreadyExists:
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, st.Message())
	case codes.InvalidArgument:
		gcperrors.InvalidArgument(w, st.Message())
	default:
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, st.Message())
	}
}
