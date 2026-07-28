package datastore

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"cloud.google.com/go/datastore/apiv1/datastorepb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// Service implements google.datastore.v1.Datastore (lab subset).
type Service struct {
	datastorepb.UnimplementedDatastoreServer

	Store         *store.Store
	Authn         *authn.Authenticator
	Authz         *authz.Evaluator
	PrincipalFrom func(context.Context) (authn.Principal, bool)
}

// Register attaches the Datastore gRPC service.
func (s *Service) Register(gs *grpc.Server) {
	datastorepb.RegisterDatastoreServer(gs, s)
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
	return s.Authn.AuthenticateToken(strings.TrimSpace(strings.TrimPrefix(raw, prefix)))
}

func (s *Service) require(ctx context.Context, permission, projectID string) error {
	p, err := s.principal(ctx)
	if err != nil {
		if err == authn.ErrUnauthenticated {
			return status.Error(codes.Unauthenticated, "unauthenticated")
		}
		if st, ok := status.FromError(err); ok {
			return st.Err()
		}
		return status.Error(codes.Unauthenticated, err.Error())
	}
	ok, err := s.Authz.Evaluate(p.Email, p.IsRoot, permission, "projects/"+projectID)
	if err != nil {
		return status.Errorf(codes.Internal, "authz: %v", err)
	}
	if !ok {
		return status.Error(codes.PermissionDenied, "The caller does not have permission.")
	}
	return nil
}

func keyPath(k *datastorepb.Key) (namespace, kind, path string, keyID int64, keyName string) {
	if k == nil {
		return "", "", "", 0, ""
	}
	if k.PartitionId != nil {
		namespace = k.PartitionId.NamespaceId
	}
	var parts []string
	for _, p := range k.Path {
		kind = p.Kind
		parts = append(parts, p.Kind)
		switch id := p.IdType.(type) {
		case *datastorepb.Key_PathElement_Id:
			keyID = id.Id
			parts = append(parts, fmt.Sprintf("id:%d", id.Id))
		case *datastorepb.Key_PathElement_Name:
			keyName = id.Name
			parts = append(parts, "name:"+id.Name)
		default:
			parts = append(parts, "incomplete")
		}
	}
	return namespace, kind, strings.Join(parts, "/"), keyID, keyName
}

func entityToStore(projectID string, e *datastorepb.Entity) (store.DatastoreEntity, error) {
	ns, kind, path, keyID, keyName := keyPath(e.GetKey())
	props := map[string]any{}
	for name, v := range e.GetProperties() {
		props[name] = valueToJSON(v)
	}
	raw, err := json.Marshal(props)
	if err != nil {
		return store.DatastoreEntity{}, err
	}
	return store.DatastoreEntity{
		ProjectID: projectID, Namespace: ns, Kind: kind, KeyPath: path,
		KeyID: keyID, KeyName: keyName, PropertiesJSON: string(raw),
	}, nil
}

func valueToJSON(v *datastorepb.Value) any {
	if v == nil {
		return nil
	}
	switch t := v.ValueType.(type) {
	case *datastorepb.Value_NullValue:
		return nil
	case *datastorepb.Value_BooleanValue:
		return t.BooleanValue
	case *datastorepb.Value_IntegerValue:
		return t.IntegerValue
	case *datastorepb.Value_DoubleValue:
		return t.DoubleValue
	case *datastorepb.Value_StringValue:
		return t.StringValue
	case *datastorepb.Value_TimestampValue:
		if t.TimestampValue != nil {
			return t.TimestampValue.AsTime().Format("2006-01-02T15:04:05.999999999Z07:00")
		}
		return nil
	default:
		b, _ := protojson.Marshal(v)
		var m any
		_ = json.Unmarshal(b, &m)
		return m
	}
}

func storeToEntity(e *store.DatastoreEntity) *datastorepb.Entity {
	key := &datastorepb.Key{
		PartitionId: &datastorepb.PartitionId{ProjectId: e.ProjectID, NamespaceId: e.Namespace},
	}
	pe := &datastorepb.Key_PathElement{Kind: e.Kind}
	if e.KeyName != "" {
		pe.IdType = &datastorepb.Key_PathElement_Name{Name: e.KeyName}
	} else {
		pe.IdType = &datastorepb.Key_PathElement_Id{Id: e.KeyID}
	}
	key.Path = []*datastorepb.Key_PathElement{pe}

	props := map[string]*datastorepb.Value{}
	var raw map[string]any
	_ = json.Unmarshal([]byte(e.PropertiesJSON), &raw)
	for k, v := range raw {
		props[k] = jsonToValue(v)
	}
	return &datastorepb.Entity{Key: key, Properties: props}
}

func jsonToValue(v any) *datastorepb.Value {
	switch t := v.(type) {
	case nil:
		return &datastorepb.Value{ValueType: &datastorepb.Value_NullValue{}}
	case bool:
		return &datastorepb.Value{ValueType: &datastorepb.Value_BooleanValue{BooleanValue: t}}
	case float64:
		if t == float64(int64(t)) {
			return &datastorepb.Value{ValueType: &datastorepb.Value_IntegerValue{IntegerValue: int64(t)}}
		}
		return &datastorepb.Value{ValueType: &datastorepb.Value_DoubleValue{DoubleValue: t}}
	case string:
		return &datastorepb.Value{ValueType: &datastorepb.Value_StringValue{StringValue: t}}
	default:
		raw, _ := json.Marshal(t)
		return &datastorepb.Value{ValueType: &datastorepb.Value_StringValue{StringValue: string(raw)}}
	}
}

// Lookup implements Datastore.Lookup.
func (s *Service) Lookup(ctx context.Context, req *datastorepb.LookupRequest) (*datastorepb.LookupResponse, error) {
	projectID := req.GetProjectId()
	if projectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	if err := s.require(ctx, "datastore.entities.get", projectID); err != nil {
		return nil, err
	}
	resp := &datastorepb.LookupResponse{}
	for _, k := range req.GetKeys() {
		ns, _, path, _, _ := keyPath(k)
		e, ok, err := s.Store.GetDatastoreEntity(projectID, ns, path)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "%v", err)
		}
		if !ok {
			resp.Missing = append(resp.Missing, &datastorepb.EntityResult{Entity: &datastorepb.Entity{Key: k}})
			continue
		}
		resp.Found = append(resp.Found, &datastorepb.EntityResult{Entity: storeToEntity(e)})
	}
	return resp, nil
}

// RunQuery supports structured equality AND filters or a GQL subset.
func (s *Service) RunQuery(ctx context.Context, req *datastorepb.RunQueryRequest) (*datastorepb.RunQueryResponse, error) {
	projectID := req.GetProjectId()
	if projectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	if err := s.require(ctx, "datastore.entities.list", projectID); err != nil {
		return nil, err
	}
	ns := ""
	if req.GetPartitionId() != nil {
		ns = req.GetPartitionId().NamespaceId
	}

	var (
		kind   string
		propEq map[string]string
		limit  int
	)
	switch {
	case req.GetQuery() != nil:
		q := req.GetQuery()
		if len(q.Kind) > 0 {
			kind = q.Kind[0].Name
		}
		propEq = map[string]string{}
		if f := q.GetFilter(); f != nil {
			if err := collectEqualityFilters(f, propEq); err != nil {
				return nil, status.Error(codes.InvalidArgument, err.Error())
			}
		}
		limit = int(q.GetLimit().GetValue())
	case req.GetGqlQuery() != nil:
		var err error
		kind, propEq, limit, err = parseGQLSubset(req.GetGqlQuery())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	default:
		return nil, status.Error(codes.InvalidArgument, "query or gql_query is required")
	}
	if kind == "" {
		return nil, status.Error(codes.InvalidArgument, "kind is required")
	}
	entities, err := s.Store.QueryDatastoreEntities(store.QueryDatastoreEntitiesFilter{
		ProjectID: projectID, Namespace: ns, Kind: kind, PropEquals: propEq, Limit: limit,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	batch := &datastorepb.QueryResultBatch{EntityResultType: datastorepb.EntityResult_FULL}
	for i := range entities {
		batch.EntityResults = append(batch.EntityResults, &datastorepb.EntityResult{Entity: storeToEntity(&entities[i])})
	}
	batch.MoreResults = datastorepb.QueryResultBatch_NO_MORE_RESULTS
	return &datastorepb.RunQueryResponse{Batch: batch}, nil
}

var (
	reGQL         = regexp.MustCompile(`(?is)^\s*SELECT\s+\*\s+FROM\s+([A-Za-z_][A-Za-z0-9_]*)(?:\s+WHERE\s+(.+?))?(?:\s+LIMIT\s+(\d+))?\s*$`)
	reGQLEq       = regexp.MustCompile(`(?i)^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.+)$`)
	reGQLAndSplit = regexp.MustCompile(`(?i)\s+AND\s+`)
)

func parseGQLSubset(g *datastorepb.GqlQuery) (kind string, propEq map[string]string, limit int, err error) {
	q := strings.TrimSpace(strings.TrimSuffix(g.GetQueryString(), ";"))
	m := reGQL.FindStringSubmatch(q)
	if m == nil {
		return "", nil, 0, fmt.Errorf("lab GQL supports: SELECT * FROM Kind [WHERE a = lit AND b = lit] [LIMIT n]")
	}
	kind = m[1]
	propEq = map[string]string{}
	if m[2] != "" {
		if !g.GetAllowLiterals() {
			return "", nil, 0, fmt.Errorf("allow_literals must be true for literal GQL filters in this lab")
		}
		parts := reGQLAndSplit.Split(m[2], -1)
		for _, part := range parts {
			em := reGQLEq.FindStringSubmatch(strings.TrimSpace(part))
			if em == nil {
				return "", nil, 0, fmt.Errorf("unsupported GQL filter clause %q", part)
			}
			rawVal := strings.TrimSpace(em[2])
			var encoded string
			if strings.HasPrefix(rawVal, "'") && strings.HasSuffix(rawVal, "'") && len(rawVal) >= 2 {
				s := rawVal[1 : len(rawVal)-1]
				b, _ := json.Marshal(s)
				encoded = string(b)
			} else if strings.HasPrefix(rawVal, `"`) && strings.HasSuffix(rawVal, `"`) && len(rawVal) >= 2 {
				s := rawVal[1 : len(rawVal)-1]
				b, _ := json.Marshal(s)
				encoded = string(b)
			} else if rawVal == "true" || rawVal == "false" {
				b, _ := json.Marshal(rawVal == "true")
				encoded = string(b)
			} else {
				var num any
				if err := json.Unmarshal([]byte(rawVal), &num); err != nil {
					return "", nil, 0, fmt.Errorf("unsupported GQL literal %q", rawVal)
				}
				b, _ := json.Marshal(num)
				encoded = string(b)
			}
			propEq[em[1]] = encoded
		}
	}
	if m[3] != "" {
		_, _ = fmt.Sscanf(m[3], "%d", &limit)
	}
	return kind, propEq, limit, nil
}

func collectEqualityFilters(f *datastorepb.Filter, out map[string]string) error {
	switch t := f.FilterType.(type) {
	case *datastorepb.Filter_PropertyFilter:
		pf := t.PropertyFilter
		if pf.GetOp() != datastorepb.PropertyFilter_EQUAL {
			return fmt.Errorf("only EQUAL property filters are supported")
		}
		name := pf.GetProperty().GetName()
		raw, _ := json.Marshal(valueToJSON(pf.GetValue()))
		out[name] = string(raw)
		return nil
	case *datastorepb.Filter_CompositeFilter:
		if t.CompositeFilter.GetOp() != datastorepb.CompositeFilter_AND {
			return fmt.Errorf("only AND composite filters are supported")
		}
		for _, sub := range t.CompositeFilter.GetFilters() {
			if err := collectEqualityFilters(sub, out); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported filter")
	}
}

// Commit applies insert/upsert/update/delete mutations. Transactional mode consumes a BeginTransaction token.
func (s *Service) Commit(ctx context.Context, req *datastorepb.CommitRequest) (*datastorepb.CommitResponse, error) {
	projectID := req.GetProjectId()
	if projectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	if err := s.require(ctx, "datastore.entities.write", projectID); err != nil {
		return nil, err
	}
	if req.GetMode() == datastorepb.CommitRequest_TRANSACTIONAL || len(req.GetTransaction()) > 0 {
		tok := req.GetTransaction()
		if len(tok) == 0 {
			return nil, status.Error(codes.InvalidArgument, "transaction is required for TRANSACTIONAL commit")
		}
		ok, err := s.Store.ConsumeDatastoreTransaction(string(tok), projectID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "%v", err)
		}
		if !ok {
			return nil, status.Error(codes.InvalidArgument, "transaction not found or already used")
		}
	}
	resp := &datastorepb.CommitResponse{}
	for _, m := range req.GetMutations() {
		switch op := m.Operation.(type) {
		case *datastorepb.Mutation_Insert:
			if err := s.putEntity(projectID, op.Insert, true); err != nil {
				return nil, err
			}
			resp.MutationResults = append(resp.MutationResults, &datastorepb.MutationResult{Key: op.Insert.GetKey()})
		case *datastorepb.Mutation_Upsert:
			if err := s.putEntity(projectID, op.Upsert, false); err != nil {
				return nil, err
			}
			resp.MutationResults = append(resp.MutationResults, &datastorepb.MutationResult{Key: op.Upsert.GetKey()})
		case *datastorepb.Mutation_Update:
			if err := s.putEntity(projectID, op.Update, false); err != nil {
				return nil, err
			}
			resp.MutationResults = append(resp.MutationResults, &datastorepb.MutationResult{Key: op.Update.GetKey()})
		case *datastorepb.Mutation_Delete:
			ns, _, path, _, _ := keyPath(op.Delete)
			if _, err := s.Store.DeleteDatastoreEntity(projectID, ns, path); err != nil {
				return nil, status.Errorf(codes.Internal, "%v", err)
			}
			resp.MutationResults = append(resp.MutationResults, &datastorepb.MutationResult{Key: op.Delete})
		default:
			return nil, status.Error(codes.InvalidArgument, "unsupported mutation")
		}
	}
	return resp, nil
}

func (s *Service) putEntity(projectID string, e *datastorepb.Entity, insertOnly bool) error {
	if e == nil || e.GetKey() == nil {
		return status.Error(codes.InvalidArgument, "entity key required")
	}
	ns, kind, path, keyID, keyName := keyPath(e.GetKey())
	incomplete := false
	if len(e.GetKey().Path) > 0 {
		last := e.GetKey().Path[len(e.GetKey().Path)-1]
		if last.GetId() == 0 && last.GetName() == "" {
			incomplete = true
		}
	}
	if incomplete {
		id, err := s.Store.NextDatastoreID(projectID, ns, kind)
		if err != nil {
			return status.Errorf(codes.Internal, "%v", err)
		}
		keyID = id
		last := e.GetKey().Path[len(e.GetKey().Path)-1]
		last.IdType = &datastorepb.Key_PathElement_Id{Id: id}
		_, kind, path, keyID, keyName = keyPath(e.GetKey())
		_ = kind
	}
	if insertOnly {
		if _, ok, err := s.Store.GetDatastoreEntity(projectID, ns, path); err != nil {
			return status.Errorf(codes.Internal, "%v", err)
		} else if ok {
			return status.Error(codes.AlreadyExists, "entity already exists")
		}
	}
	se, err := entityToStore(projectID, e)
	if err != nil {
		return status.Errorf(codes.Internal, "%v", err)
	}
	se.KeyID, se.KeyName = keyID, keyName
	if err := s.Store.PutDatastoreEntity(se); err != nil {
		return status.Errorf(codes.Internal, "%v", err)
	}
	return nil
}

// AllocateIds allocates numeric ids for incomplete keys.
func (s *Service) AllocateIds(ctx context.Context, req *datastorepb.AllocateIdsRequest) (*datastorepb.AllocateIdsResponse, error) {
	projectID := req.GetProjectId()
	if projectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	if err := s.require(ctx, "datastore.entities.write", projectID); err != nil {
		return nil, err
	}
	if len(req.GetKeys()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "keys is required")
	}
	out := make([]*datastorepb.Key, 0, len(req.GetKeys()))
	for _, k := range req.GetKeys() {
		if k == nil || len(k.Path) == 0 {
			return nil, status.Error(codes.InvalidArgument, "incomplete key required")
		}
		last := k.Path[len(k.Path)-1]
		if last.GetId() != 0 || last.GetName() != "" {
			return nil, status.Error(codes.InvalidArgument, "AllocateIds requires incomplete keys")
		}
		ns, kind, _, _, _ := keyPath(k)
		id, err := s.Store.NextDatastoreID(projectID, ns, kind)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "%v", err)
		}
		// Reserve the id so subsequent allocations advance.
		path := kind + "/id:" + fmt.Sprintf("%d", id)
		if err := s.Store.PutDatastoreEntity(store.DatastoreEntity{
			ProjectID: projectID, Namespace: ns, Kind: kind, KeyPath: path,
			KeyID: id, PropertiesJSON: "{}",
		}); err != nil {
			return nil, status.Errorf(codes.Internal, "%v", err)
		}
		nk := &datastorepb.Key{
			PartitionId: k.PartitionId,
			Path:        make([]*datastorepb.Key_PathElement, len(k.Path)),
		}
		if nk.PartitionId == nil {
			nk.PartitionId = &datastorepb.PartitionId{ProjectId: projectID}
		} else if nk.PartitionId.ProjectId == "" {
			nk.PartitionId.ProjectId = projectID
		}
		for i, pe := range k.Path {
			cp := &datastorepb.Key_PathElement{Kind: pe.Kind}
			if i == len(k.Path)-1 {
				cp.IdType = &datastorepb.Key_PathElement_Id{Id: id}
			} else {
				cp.IdType = pe.IdType
			}
			nk.Path[i] = cp
		}
		out = append(out, nk)
	}
	return &datastorepb.AllocateIdsResponse{Keys: out}, nil
}

// BeginTransaction returns a lab UUID token. No isolation is provided.
func (s *Service) BeginTransaction(ctx context.Context, req *datastorepb.BeginTransactionRequest) (*datastorepb.BeginTransactionResponse, error) {
	projectID := req.GetProjectId()
	if projectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	if err := s.require(ctx, "datastore.entities.write", projectID); err != nil {
		return nil, err
	}
	token := uuid.NewString()
	if err := s.Store.PutDatastoreTransaction(token, projectID, req.GetDatabaseId()); err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return &datastorepb.BeginTransactionResponse{Transaction: []byte(token)}, nil
}

// Rollback clears a lab transaction token.
func (s *Service) Rollback(ctx context.Context, req *datastorepb.RollbackRequest) (*datastorepb.RollbackResponse, error) {
	projectID := req.GetProjectId()
	if projectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	if len(req.GetTransaction()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "transaction is required")
	}
	if err := s.require(ctx, "datastore.entities.write", projectID); err != nil {
		return nil, err
	}
	ok, err := s.Store.DeleteDatastoreTransaction(string(req.GetTransaction()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "transaction not found")
	}
	return &datastorepb.RollbackResponse{}, nil
}
