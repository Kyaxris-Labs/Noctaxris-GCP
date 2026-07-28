package firestore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore/apiv1/firestorepb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Service implements google.firestore.v1.Firestore for lab depth.
type Service struct {
	firestorepb.UnimplementedFirestoreServer
	Store *store.Store
	Authn *authn.Authenticator
	Authz *authz.Evaluator
	// PrincipalFrom returns a principal already attached to ctx (e.g. by a gRPC interceptor).
	PrincipalFrom func(context.Context) (authn.Principal, bool)
}

func (s *Service) principal(ctx context.Context) (authn.Principal, error) {
	if s.PrincipalFrom != nil {
		if p, ok := s.PrincipalFrom(ctx); ok {
			return p, nil
		}
	}
	if s.Authn == nil {
		return authn.Principal{}, status.Error(codes.Unauthenticated, "authenticator not configured")
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return authn.Principal{}, status.Error(codes.Unauthenticated, "missing metadata")
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return authn.Principal{}, status.Error(codes.Unauthenticated, "missing authorization")
	}
	raw := vals[0]
	const prefix = "Bearer "
	if !strings.HasPrefix(raw, prefix) {
		return authn.Principal{}, status.Error(codes.Unauthenticated, "expected Bearer token")
	}
	p, err := s.Authn.AuthenticateToken(strings.TrimSpace(strings.TrimPrefix(raw, prefix)))
	if err != nil {
		return authn.Principal{}, err
	}
	return p, nil
}

func (s *Service) require(ctx context.Context, permission, projectID string) (authn.Principal, error) {
	p, err := s.principal(ctx)
	if err != nil {
		if err == authn.ErrUnauthenticated {
			return authn.Principal{}, status.Error(codes.Unauthenticated, "unauthenticated")
		}
		if st, ok := status.FromError(err); ok {
			return authn.Principal{}, st.Err()
		}
		return authn.Principal{}, status.Error(codes.Unauthenticated, err.Error())
	}
	resource := "projects/" + projectID
	ok, err := s.Authz.Evaluate(p.Email, p.IsRoot, permission, resource)
	if err != nil {
		return authn.Principal{}, status.Errorf(codes.Internal, "authz: %v", err)
	}
	if !ok {
		return authn.Principal{}, status.Error(codes.PermissionDenied, "The caller does not have permission.")
	}
	return p, nil
}

func projectFromName(name string) (string, error) {
	parts := strings.Split(name, "/")
	if len(parts) < 2 || parts[0] != "projects" || parts[1] == "" {
		return "", fmt.Errorf("invalid resource name %q", name)
	}
	return parts[1], nil
}

func parseDocPath(name string) (projectID, collectionID, documentID string, err error) {
	// projects/{p}/databases/(default)/documents/{collection}/{doc}[/...]
	const marker = "/documents/"
	i := strings.Index(name, marker)
	if i < 0 || !strings.HasPrefix(name, "projects/") {
		return "", "", "", fmt.Errorf("invalid document name %q", name)
	}
	projectID, err = projectFromName(name)
	if err != nil {
		return "", "", "", err
	}
	rest := name[i+len(marker):]
	segs := strings.Split(rest, "/")
	if len(segs) < 2 || len(segs)%2 != 0 {
		return "", "", "", fmt.Errorf("invalid document path in %q", name)
	}
	collectionID = segs[len(segs)-2]
	documentID = segs[len(segs)-1]
	return projectID, collectionID, documentID, nil
}

func fieldsToJSON(fields map[string]*firestorepb.Value) (string, error) {
	if fields == nil {
		return "{}", nil
	}
	wrapper := &firestorepb.Document{Fields: fields}
	b, err := protojson.MarshalOptions{EmitUnpopulated: false}.Marshal(wrapper)
	if err != nil {
		return "", err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return "", err
	}
	f, ok := raw["fields"]
	if !ok {
		return "{}", nil
	}
	return string(f), nil
}

func fieldsFromJSON(raw string) (map[string]*firestorepb.Value, error) {
	if raw == "" || raw == "{}" {
		return map[string]*firestorepb.Value{}, nil
	}
	doc := &firestorepb.Document{}
	if err := protojson.Unmarshal([]byte(`{"fields":`+raw+`}`), doc); err != nil {
		return nil, err
	}
	if doc.Fields == nil {
		return map[string]*firestorepb.Value{}, nil
	}
	return doc.Fields, nil
}

func parseTime(s string) *timestamppb.Timestamp {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return timestamppb.Now()
		}
	}
	return timestamppb.New(t)
}

func (s *Service) toProto(d store.FirestoreDoc) (*firestorepb.Document, error) {
	fields, err := fieldsFromJSON(d.FieldsJSON)
	if err != nil {
		return nil, err
	}
	return &firestorepb.Document{
		Name:       d.Path,
		Fields:     fields,
		CreateTime: parseTime(d.CreateTime),
		UpdateTime: parseTime(d.UpdateTime),
	}, nil
}

func newDocID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// GetDocument implements Firestore.GetDocument.
func (s *Service) GetDocument(ctx context.Context, req *firestorepb.GetDocumentRequest) (*firestorepb.Document, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	projectID, _, _, err := parseDocPath(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if _, err := s.require(ctx, "datastore.entities.get", projectID); err != nil {
		return nil, err
	}
	d, ok, err := s.Store.GetFirestoreDoc(req.GetName())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return nil, status.Errorf(codes.NotFound, "Document '%s' not found", req.GetName())
	}
	return s.toProto(d)
}

// CreateDocument implements Firestore.CreateDocument.
func (s *Service) CreateDocument(ctx context.Context, req *firestorepb.CreateDocumentRequest) (*firestorepb.Document, error) {
	parent := req.GetParent()
	coll := req.GetCollectionId()
	if parent == "" || coll == "" || req.GetDocument() == nil {
		return nil, status.Error(codes.InvalidArgument, "parent, collection_id, and document are required")
	}
	projectID, err := projectFromName(parent)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if _, err := s.require(ctx, "datastore.entities.create", projectID); err != nil {
		return nil, err
	}
	docID := req.GetDocumentId()
	if docID == "" {
		docID = newDocID()
	}
	path := strings.TrimSuffix(parent, "/") + "/" + coll + "/" + docID
	if _, ok, err := s.Store.GetFirestoreDoc(path); err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	} else if ok {
		return nil, status.Errorf(codes.AlreadyExists, "Document already exists: %s", path)
	}
	fieldsJSON, err := fieldsToJSON(req.GetDocument().GetFields())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "fields: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	d := store.FirestoreDoc{
		Path: path, ProjectID: projectID, CollectionID: coll, DocumentID: docID,
		FieldsJSON: fieldsJSON, CreateTime: now, UpdateTime: now,
	}
	if err := s.Store.PutFirestoreDoc(d); err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return s.toProto(d)
}

// UpdateDocument implements Firestore.UpdateDocument.
func (s *Service) UpdateDocument(ctx context.Context, req *firestorepb.UpdateDocumentRequest) (*firestorepb.Document, error) {
	doc := req.GetDocument()
	if doc == nil || doc.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "document.name is required")
	}
	projectID, coll, docID, err := parseDocPath(doc.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if _, err := s.require(ctx, "datastore.entities.update", projectID); err != nil {
		return nil, err
	}
	existing, ok, err := s.Store.GetFirestoreDoc(doc.GetName())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return nil, status.Errorf(codes.NotFound, "Document '%s' not found", doc.GetName())
	}
	fieldsJSON, err := fieldsToJSON(doc.GetFields())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "fields: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	d := store.FirestoreDoc{
		Path: doc.GetName(), ProjectID: projectID, CollectionID: coll, DocumentID: docID,
		FieldsJSON: fieldsJSON, CreateTime: existing.CreateTime, UpdateTime: now,
	}
	if err := s.Store.PutFirestoreDoc(d); err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return s.toProto(d)
}

// DeleteDocument implements Firestore.DeleteDocument.
func (s *Service) DeleteDocument(ctx context.Context, req *firestorepb.DeleteDocumentRequest) (*emptypb.Empty, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	projectID, _, _, err := parseDocPath(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if _, err := s.require(ctx, "datastore.entities.delete", projectID); err != nil {
		return nil, err
	}
	ok, err := s.Store.DeleteFirestoreDoc(req.GetName())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return nil, status.Errorf(codes.NotFound, "Document '%s' not found", req.GetName())
	}
	return &emptypb.Empty{}, nil
}

// ListDocuments implements Firestore.ListDocuments.
func (s *Service) ListDocuments(ctx context.Context, req *firestorepb.ListDocumentsRequest) (*firestorepb.ListDocumentsResponse, error) {
	parent := req.GetParent()
	coll := req.GetCollectionId()
	if parent == "" || coll == "" {
		return nil, status.Error(codes.InvalidArgument, "parent and collection_id are required")
	}
	projectID, err := projectFromName(parent)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if _, err := s.require(ctx, "datastore.entities.list", projectID); err != nil {
		return nil, err
	}
	pageSize := int(req.GetPageSize())
	docs, err := s.Store.ListFirestoreDocs(projectID, parent, coll, pageSize)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	out := make([]*firestorepb.Document, 0, len(docs))
	for _, d := range docs {
		p, err := s.toProto(d)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "%v", err)
		}
		out = append(out, p)
	}
	return &firestorepb.ListDocumentsResponse{Documents: out}, nil
}

// BatchGetDocuments implements Firestore.BatchGetDocuments (server streaming).
func (s *Service) BatchGetDocuments(req *firestorepb.BatchGetDocumentsRequest, stream firestorepb.Firestore_BatchGetDocumentsServer) error {
	if len(req.GetDocuments()) == 0 {
		return status.Error(codes.InvalidArgument, "documents is required")
	}
	projectID, err := projectFromName(req.GetDocuments()[0])
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if _, err := s.require(stream.Context(), "datastore.entities.get", projectID); err != nil {
		return err
	}
	readTime := timestamppb.Now()
	for _, name := range req.GetDocuments() {
		d, ok, err := s.Store.GetFirestoreDoc(name)
		if err != nil {
			return status.Errorf(codes.Internal, "%v", err)
		}
		if !ok {
			if err := stream.Send(&firestorepb.BatchGetDocumentsResponse{
				Result:   &firestorepb.BatchGetDocumentsResponse_Missing{Missing: name},
				ReadTime: readTime,
			}); err != nil {
				return err
			}
			continue
		}
		p, err := s.toProto(d)
		if err != nil {
			return status.Errorf(codes.Internal, "%v", err)
		}
		if err := stream.Send(&firestorepb.BatchGetDocumentsResponse{
			Result:   &firestorepb.BatchGetDocumentsResponse_Found{Found: p},
			ReadTime: readTime,
		}); err != nil {
			return err
		}
	}
	return nil
}

// BatchWrite implements Firestore.BatchWrite (non-transactional writes).
func (s *Service) BatchWrite(ctx context.Context, req *firestorepb.BatchWriteRequest) (*firestorepb.BatchWriteResponse, error) {
	if req.GetDatabase() == "" {
		return nil, status.Error(codes.InvalidArgument, "database is required")
	}
	projectID, err := projectFromName(req.GetDatabase())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if _, err := s.require(ctx, "datastore.entities.create", projectID); err != nil {
		return nil, err
	}
	statusOut := make([]*rpcstatus.Status, 0, len(req.GetWrites()))
	writeResults := make([]*firestorepb.WriteResult, 0, len(req.GetWrites()))
	for _, w := range req.GetWrites() {
		wr, st := s.applyWrite(projectID, w)
		statusOut = append(statusOut, st)
		writeResults = append(writeResults, wr)
	}
	return &firestorepb.BatchWriteResponse{WriteResults: writeResults, Status: statusOut}, nil
}

func (s *Service) applyWrite(projectID string, w *firestorepb.Write) (*firestorepb.WriteResult, *rpcstatus.Status) {
	now := timestamppb.Now()
	wr := &firestorepb.WriteResult{UpdateTime: now}
	switch op := w.GetOperation().(type) {
	case *firestorepb.Write_Update:
		doc := op.Update
		if doc == nil || doc.GetName() == "" {
			return wr, &rpcstatus.Status{Code: int32(codes.InvalidArgument), Message: "update name required"}
		}
		_, coll, docID, err := parseDocPath(doc.GetName())
		if err != nil {
			return wr, &rpcstatus.Status{Code: int32(codes.InvalidArgument), Message: err.Error()}
		}
		fieldsJSON, err := fieldsToJSON(doc.GetFields())
		if err != nil {
			return wr, &rpcstatus.Status{Code: int32(codes.InvalidArgument), Message: err.Error()}
		}
		existing, ok, err := s.Store.GetFirestoreDoc(doc.GetName())
		if err != nil {
			return wr, &rpcstatus.Status{Code: int32(codes.Internal), Message: err.Error()}
		}
		createTime := now.AsTime().UTC().Format(time.RFC3339Nano)
		if ok {
			createTime = existing.CreateTime
		}
		d := store.FirestoreDoc{
			Path: doc.GetName(), ProjectID: projectID, CollectionID: coll, DocumentID: docID,
			FieldsJSON: fieldsJSON, CreateTime: createTime, UpdateTime: createTime,
		}
		if ok {
			d.UpdateTime = now.AsTime().UTC().Format(time.RFC3339Nano)
		}
		if err := s.Store.PutFirestoreDoc(d); err != nil {
			return wr, &rpcstatus.Status{Code: int32(codes.Internal), Message: err.Error()}
		}
		return wr, &rpcstatus.Status{Code: int32(codes.OK)}
	case *firestorepb.Write_Delete:
		ok, err := s.Store.DeleteFirestoreDoc(op.Delete)
		if err != nil {
			return wr, &rpcstatus.Status{Code: int32(codes.Internal), Message: err.Error()}
		}
		if !ok {
			return wr, &rpcstatus.Status{Code: int32(codes.NotFound), Message: "not found"}
		}
		return wr, &rpcstatus.Status{Code: int32(codes.OK)}
	default:
		return wr, &rpcstatus.Status{Code: int32(codes.Unimplemented), Message: "write operation not supported in lab"}
	}
}

// Commit applies writes without transaction semantics (lab).
func (s *Service) Commit(ctx context.Context, req *firestorepb.CommitRequest) (*firestorepb.CommitResponse, error) {
	if req.GetDatabase() == "" {
		return nil, status.Error(codes.InvalidArgument, "database is required")
	}
	projectID, err := projectFromName(req.GetDatabase())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if _, err := s.require(ctx, "datastore.entities.update", projectID); err != nil {
		return nil, err
	}
	if len(req.GetTransaction()) > 0 {
		return nil, status.Error(codes.Unimplemented, "transactions are not implemented in this lab emulator")
	}
	results := make([]*firestorepb.WriteResult, 0, len(req.GetWrites()))
	for _, w := range req.GetWrites() {
		wr, st := s.applyWrite(projectID, w)
		if st.GetCode() != int32(codes.OK) {
			return nil, status.Error(codes.Code(st.GetCode()), st.GetMessage())
		}
		results = append(results, wr)
	}
	return &firestorepb.CommitResponse{WriteResults: results, CommitTime: timestamppb.Now()}, nil
}

// RunQuery implements a lab subset: collection equality FieldFilter.
func (s *Service) RunQuery(req *firestorepb.RunQueryRequest, stream firestorepb.Firestore_RunQueryServer) error {
	parent := req.GetParent()
	if parent == "" {
		return status.Error(codes.InvalidArgument, "parent is required")
	}
	projectID, err := projectFromName(parent)
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if _, err := s.require(stream.Context(), "datastore.entities.list", projectID); err != nil {
		return err
	}
	sq := req.GetStructuredQuery()
	if sq == nil || len(sq.GetFrom()) == 0 {
		return status.Error(codes.InvalidArgument, "structured_query.from is required")
	}
	coll := sq.GetFrom()[0].GetCollectionId()
	fieldName, want, okFilter := extractEqualityFilter(sq.GetWhere())
	docs, err := s.Store.ListFirestoreDocs(projectID, parent, coll, 1000)
	if err != nil {
		return status.Errorf(codes.Internal, "%v", err)
	}
	readTime := timestamppb.Now()
	for _, d := range docs {
		if okFilter {
			match, err := fieldEquals(d.FieldsJSON, fieldName, want)
			if err != nil {
				return status.Errorf(codes.Internal, "%v", err)
			}
			if !match {
				continue
			}
		}
		p, err := s.toProto(d)
		if err != nil {
			return status.Errorf(codes.Internal, "%v", err)
		}
		if err := stream.Send(&firestorepb.RunQueryResponse{Document: p, ReadTime: readTime}); err != nil {
			return err
		}
	}
	return nil
}

func extractEqualityFilter(filter *firestorepb.StructuredQuery_Filter) (field string, want *firestorepb.Value, ok bool) {
	if filter == nil {
		return "", nil, false
	}
	ff := filter.GetFieldFilter()
	if ff == nil {
		return "", nil, false
	}
	if ff.GetOp() != firestorepb.StructuredQuery_FieldFilter_EQUAL {
		return "", nil, false
	}
	return ff.GetField().GetFieldPath(), ff.GetValue(), true
}

func fieldEquals(fieldsJSON, field string, want *firestorepb.Value) (bool, error) {
	fields, err := fieldsFromJSON(fieldsJSON)
	if err != nil {
		return false, err
	}
	got, ok := fields[field]
	if !ok {
		return false, nil
	}
	gb, err := protojson.Marshal(got)
	if err != nil {
		return false, err
	}
	wb, err := protojson.Marshal(want)
	if err != nil {
		return false, err
	}
	return string(gb) == string(wb), nil
}

// BeginTransaction is not implemented in Wave 1.
func (s *Service) BeginTransaction(context.Context, *firestorepb.BeginTransactionRequest) (*firestorepb.BeginTransactionResponse, error) {
	return nil, gcperrors.GRPC(gcperrors.StatusUnimplemented, "BeginTransaction is not implemented in this lab emulator")
}

// Listen is not implemented in Wave 1.
func (s *Service) Listen(firestorepb.Firestore_ListenServer) error {
	return gcperrors.GRPC(gcperrors.StatusUnimplemented, "Listen is not implemented in this lab emulator")
}
