package datastore_test

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"cloud.google.com/go/datastore/apiv1/datastorepb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	dsvc "github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/datastore"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func startDatastore(t *testing.T) (datastorepb.DatastoreClient, func()) {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	project := "noctaxris-gcp-local"
	rootSA := "root@noctaxris-gcp-local.iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, rootSA); err != nil {
		t.Fatal(err)
	}
	svc := &dsvc.Service{
		Store: st,
		Authn: &authn.Authenticator{RootServiceAccount: rootSA, RootAccessToken: "test-root-token"},
		Authz: &authz.Evaluator{Policies: st},
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	svc.Register(gs)
	go func() { _ = gs.Serve(lis) }()
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		_ = conn.Close()
		gs.Stop()
		_ = st.Close()
	}
	return datastorepb.NewDatastoreClient(conn), cleanup
}

func authCtx(token string) context.Context {
	return metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
}

func taskKey(project, name string) *datastorepb.Key {
	return &datastorepb.Key{
		PartitionId: &datastorepb.PartitionId{ProjectId: project},
		Path: []*datastorepb.Key_PathElement{{
			Kind: "Task", IdType: &datastorepb.Key_PathElement_Name{Name: name},
		}},
	}
}

func TestCommitUpsertLookupAndDelete(t *testing.T) {
	client, cleanup := startDatastore(t)
	defer cleanup()
	ctx := authCtx("test-root-token")
	project := "noctaxris-gcp-local"
	key := taskKey(project, "t1")
	ent := &datastorepb.Entity{
		Key: key,
		Properties: map[string]*datastorepb.Value{
			"title": {ValueType: &datastorepb.Value_StringValue{StringValue: "lab"}},
		},
	}

	_, err := client.Commit(ctx, &datastorepb.CommitRequest{
		ProjectId: project,
		Mode:      datastorepb.CommitRequest_NON_TRANSACTIONAL,
		Mutations: []*datastorepb.Mutation{{Operation: &datastorepb.Mutation_Upsert{Upsert: ent}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	lookup, err := client.Lookup(ctx, &datastorepb.LookupRequest{
		ProjectId: project,
		Keys:      []*datastorepb.Key{key, taskKey(project, "missing")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(lookup.Found) != 1 || len(lookup.Missing) != 1 {
		t.Fatalf("found=%d missing=%d", len(lookup.Found), len(lookup.Missing))
	}
	if lookup.Found[0].Entity.Properties["title"].GetStringValue() != "lab" {
		t.Fatalf("title=%#v", lookup.Found[0].Entity.Properties["title"])
	}

	_, err = client.Commit(ctx, &datastorepb.CommitRequest{
		ProjectId: project,
		Mode:      datastorepb.CommitRequest_NON_TRANSACTIONAL,
		Mutations: []*datastorepb.Mutation{{Operation: &datastorepb.Mutation_Delete{Delete: key}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := client.Lookup(ctx, &datastorepb.LookupRequest{ProjectId: project, Keys: []*datastorepb.Key{key}})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Missing) != 1 {
		t.Fatalf("expected missing after delete, found=%v", after.Found)
	}
}

func TestCommitInsertOnlyRejectsDuplicate(t *testing.T) {
	client, cleanup := startDatastore(t)
	defer cleanup()
	ctx := authCtx("test-root-token")
	project := "noctaxris-gcp-local"
	key := taskKey(project, "dup")
	ent := &datastorepb.Entity{
		Key: key,
		Properties: map[string]*datastorepb.Value{
			"v": {ValueType: &datastorepb.Value_IntegerValue{IntegerValue: 1}},
		},
	}
	_, err := client.Commit(ctx, &datastorepb.CommitRequest{
		ProjectId: project,
		Mode:      datastorepb.CommitRequest_NON_TRANSACTIONAL,
		Mutations: []*datastorepb.Mutation{{Operation: &datastorepb.Mutation_Insert{Insert: ent}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Commit(ctx, &datastorepb.CommitRequest{
		ProjectId: project,
		Mode:      datastorepb.CommitRequest_NON_TRANSACTIONAL,
		Mutations: []*datastorepb.Mutation{{Operation: &datastorepb.Mutation_Insert{Insert: ent}}},
	})
	if err == nil {
		t.Fatal("expected AlreadyExists on second insert")
	}
}

func TestRunQueryStructuredAndGQL(t *testing.T) {
	client, cleanup := startDatastore(t)
	defer cleanup()
	ctx := authCtx("test-root-token")
	project := "noctaxris-gcp-local"

	for _, pair := range []struct {
		name, color string
		size        int64
	}{
		{"g1", "red", 1},
		{"g2", "blue", 1},
	} {
		key := taskKey(project, pair.name)
		key.Path[0].Kind = "Item"
		ent := &datastorepb.Entity{
			Key: key,
			Properties: map[string]*datastorepb.Value{
				"color": {ValueType: &datastorepb.Value_StringValue{StringValue: pair.color}},
				"size":  {ValueType: &datastorepb.Value_IntegerValue{IntegerValue: pair.size}},
			},
		}
		if _, err := client.Commit(ctx, &datastorepb.CommitRequest{
			ProjectId: project,
			Mode:      datastorepb.CommitRequest_NON_TRANSACTIONAL,
			Mutations: []*datastorepb.Mutation{{Operation: &datastorepb.Mutation_Upsert{Upsert: ent}}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	gql, err := client.RunQuery(ctx, &datastorepb.RunQueryRequest{
		ProjectId: project,
		QueryType: &datastorepb.RunQueryRequest_GqlQuery{
			GqlQuery: &datastorepb.GqlQuery{
				QueryString:   "SELECT * FROM Item WHERE color = 'red' AND size = 1 LIMIT 5",
				AllowLiterals: true,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(gql.GetBatch().GetEntityResults()) != 1 {
		t.Fatalf("gql results=%d", len(gql.GetBatch().GetEntityResults()))
	}

	andResp, err := client.RunQuery(ctx, &datastorepb.RunQueryRequest{
		ProjectId: project,
		QueryType: &datastorepb.RunQueryRequest_Query{
			Query: &datastorepb.Query{
				Kind:  []*datastorepb.KindExpression{{Name: "Item"}},
				Limit: wrapperspb.Int32(10),
				Filter: &datastorepb.Filter{FilterType: &datastorepb.Filter_CompositeFilter{
					CompositeFilter: &datastorepb.CompositeFilter{
						Op: datastorepb.CompositeFilter_AND,
						Filters: []*datastorepb.Filter{
							{FilterType: &datastorepb.Filter_PropertyFilter{PropertyFilter: &datastorepb.PropertyFilter{
								Property: &datastorepb.PropertyReference{Name: "color"},
								Op:       datastorepb.PropertyFilter_EQUAL,
								Value:    &datastorepb.Value{ValueType: &datastorepb.Value_StringValue{StringValue: "blue"}},
							}}},
							{FilterType: &datastorepb.Filter_PropertyFilter{PropertyFilter: &datastorepb.PropertyFilter{
								Property: &datastorepb.PropertyReference{Name: "size"},
								Op:       datastorepb.PropertyFilter_EQUAL,
								Value:    &datastorepb.Value{ValueType: &datastorepb.Value_IntegerValue{IntegerValue: 1}},
							}}},
						},
					},
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(andResp.GetBatch().GetEntityResults()) != 1 {
		t.Fatalf("and results=%d", len(andResp.GetBatch().GetEntityResults()))
	}
}

func TestAllocateIdsAndTransactionalCommit(t *testing.T) {
	client, cleanup := startDatastore(t)
	defer cleanup()
	ctx := authCtx("test-root-token")
	project := "noctaxris-gcp-local"

	alloc, err := client.AllocateIds(ctx, &datastorepb.AllocateIdsRequest{
		ProjectId: project,
		Keys: []*datastorepb.Key{{
			PartitionId: &datastorepb.PartitionId{ProjectId: project},
			Path:        []*datastorepb.Key_PathElement{{Kind: "Auto"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(alloc.GetKeys()) != 1 || alloc.GetKeys()[0].Path[0].GetId() == 0 {
		t.Fatalf("alloc=%v", alloc.GetKeys())
	}

	begun, err := client.BeginTransaction(ctx, &datastorepb.BeginTransactionRequest{ProjectId: project})
	if err != nil {
		t.Fatal(err)
	}
	txKey := taskKey(project, "tx-one")
	txKey.Path[0].Kind = "Tx"
	_, err = client.Commit(ctx, &datastorepb.CommitRequest{
		ProjectId: project,
		Mode:      datastorepb.CommitRequest_TRANSACTIONAL,
		TransactionSelector: &datastorepb.CommitRequest_Transaction{
			Transaction: begun.GetTransaction(),
		},
		Mutations: []*datastorepb.Mutation{{
			Operation: &datastorepb.Mutation_Upsert{
				Upsert: &datastorepb.Entity{
					Key: txKey,
					Properties: map[string]*datastorepb.Value{
						"ok": {ValueType: &datastorepb.Value_BooleanValue{BooleanValue: true}},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := client.Lookup(ctx, &datastorepb.LookupRequest{ProjectId: project, Keys: []*datastorepb.Key{txKey}})
	if err != nil {
		t.Fatal(err)
	}
	if len(lookup.Found) != 1 {
		t.Fatalf("found=%v", lookup.Found)
	}

	_, err = client.Commit(ctx, &datastorepb.CommitRequest{
		ProjectId: project,
		Mode:      datastorepb.CommitRequest_TRANSACTIONAL,
		TransactionSelector: &datastorepb.CommitRequest_Transaction{
			Transaction: begun.GetTransaction(),
		},
	})
	if err == nil {
		t.Fatal("expected reused transaction token to fail")
	}

	begun2, err := client.BeginTransaction(ctx, &datastorepb.BeginTransactionRequest{ProjectId: project})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Rollback(ctx, &datastorepb.RollbackRequest{
		ProjectId: project, Transaction: begun2.GetTransaction(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Commit(ctx, &datastorepb.CommitRequest{
		ProjectId: project,
		Mode:      datastorepb.CommitRequest_TRANSACTIONAL,
		TransactionSelector: &datastorepb.CommitRequest_Transaction{
			Transaction: begun2.GetTransaction(),
		},
	})
	if err == nil {
		t.Fatal("expected rolled-back transaction to fail commit")
	}
}

func TestDatastoreAuthzDenyNonRoot(t *testing.T) {
	dir := t.TempDir()
	master, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), master)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	project := "noctaxris-gcp-local"
	if err := st.EnsureRoot(project, "root@"+project+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	svc := &dsvc.Service{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		PrincipalFrom: func(context.Context) (authn.Principal, bool) {
			return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
		},
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	svc.Register(gs)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(func() { gs.Stop() })
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := datastorepb.NewDatastoreClient(conn)
	entKey := taskKey(project, "deny-1")
	_, err = client.Commit(context.Background(), &datastorepb.CommitRequest{
		ProjectId: project,
		Mode:      datastorepb.CommitRequest_NON_TRANSACTIONAL,
		Mutations: []*datastorepb.Mutation{{
			Operation: &datastorepb.Mutation_Upsert{
				Upsert: &datastorepb.Entity{
					Key: entKey,
					Properties: map[string]*datastorepb.Value{
						"v": {ValueType: &datastorepb.Value_StringValue{StringValue: "no"}},
					},
				},
			},
		}},
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", err)
	}
}
