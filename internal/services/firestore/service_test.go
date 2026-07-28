package firestore_test

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"testing"

	"cloud.google.com/go/firestore/apiv1/firestorepb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	fsvc "github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/firestore"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func startFirestore(t *testing.T) (firestorepb.FirestoreClient, *store.Store, func()) {
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
	svc := &fsvc.Service{
		Store: st,
		Authn: &authn.Authenticator{RootServiceAccount: rootSA, RootAccessToken: "test-root-token"},
		Authz: &authz.Evaluator{Policies: st},
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	firestorepb.RegisterFirestoreServer(gs, svc)
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
	return firestorepb.NewFirestoreClient(conn), st, cleanup
}

func authCtx(token string) context.Context {
	return metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
}

func TestCreateAndGetDocument(t *testing.T) {
	client, _, cleanup := startFirestore(t)
	defer cleanup()

	parent := "projects/noctaxris-gcp-local/databases/(default)/documents"
	ctx := authCtx("test-root-token")
	created, err := client.CreateDocument(ctx, &firestorepb.CreateDocumentRequest{
		Parent:       parent,
		CollectionId: "users",
		DocumentId:   "ada",
		Document: &firestorepb.Document{
			Fields: map[string]*firestorepb.Value{
				"name": {ValueType: &firestorepb.Value_StringValue{StringValue: "Ada"}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantName := parent + "/users/ada"
	if created.GetName() != wantName {
		t.Fatalf("name = %q", created.GetName())
	}
	got, err := client.GetDocument(ctx, &firestorepb.GetDocumentRequest{Name: wantName})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetFields()["name"].GetStringValue() != "Ada" {
		t.Fatalf("fields = %#v", got.GetFields())
	}
}

func TestUpdateDocumentFieldMask(t *testing.T) {
	client, _, cleanup := startFirestore(t)
	defer cleanup()
	parent := "projects/noctaxris-gcp-local/databases/(default)/documents"
	ctx := authCtx("test-root-token")
	name := parent + "/users/mask"
	_, err := client.CreateDocument(ctx, &firestorepb.CreateDocumentRequest{
		Parent: parent, CollectionId: "users", DocumentId: "mask",
		Document: &firestorepb.Document{Fields: map[string]*firestorepb.Value{
			"name":  {ValueType: &firestorepb.Value_StringValue{StringValue: "Ada"}},
			"city":  {ValueType: &firestorepb.Value_StringValue{StringValue: "London"}},
			"score": {ValueType: &firestorepb.Value_IntegerValue{IntegerValue: 1}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := client.UpdateDocument(ctx, &firestorepb.UpdateDocumentRequest{
		Document: &firestorepb.Document{
			Name: name,
			Fields: map[string]*firestorepb.Value{
				"city": {ValueType: &firestorepb.Value_StringValue{StringValue: "Paris"}},
			},
		},
		UpdateMask: &firestorepb.DocumentMask{FieldPaths: []string{"city"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GetFields()["name"].GetStringValue() != "Ada" {
		t.Fatalf("name should be preserved: %#v", updated.GetFields())
	}
	if updated.GetFields()["city"].GetStringValue() != "Paris" {
		t.Fatalf("city = %#v", updated.GetFields()["city"])
	}
	if updated.GetFields()["score"].GetIntegerValue() != 1 {
		t.Fatalf("score should be preserved: %#v", updated.GetFields())
	}
}

func TestRunQueryINAndArrayContains(t *testing.T) {
	client, _, cleanup := startFirestore(t)
	defer cleanup()
	parent := "projects/noctaxris-gcp-local/databases/(default)/documents"
	ctx := authCtx("test-root-token")
	for _, pair := range []struct {
		id, city string
		tags     []string
	}{
		{"a", "London", []string{"eng", "eu"}},
		{"b", "Paris", []string{"fr"}},
		{"c", "Berlin", []string{"de", "eu"}},
	} {
		fields := map[string]*firestorepb.Value{
			"city": {ValueType: &firestorepb.Value_StringValue{StringValue: pair.city}},
		}
		vals := make([]*firestorepb.Value, 0, len(pair.tags))
		for _, tag := range pair.tags {
			vals = append(vals, &firestorepb.Value{ValueType: &firestorepb.Value_StringValue{StringValue: tag}})
		}
		fields["tags"] = &firestorepb.Value{ValueType: &firestorepb.Value_ArrayValue{ArrayValue: &firestorepb.ArrayValue{Values: vals}}}
		if _, err := client.CreateDocument(ctx, &firestorepb.CreateDocumentRequest{
			Parent: parent, CollectionId: "cities", DocumentId: pair.id,
			Document: &firestorepb.Document{Fields: fields},
		}); err != nil {
			t.Fatal(err)
		}
	}

	inStream, err := client.RunQuery(ctx, &firestorepb.RunQueryRequest{
		Parent: parent,
		QueryType: &firestorepb.RunQueryRequest_StructuredQuery{
			StructuredQuery: &firestorepb.StructuredQuery{
				From: []*firestorepb.StructuredQuery_CollectionSelector{{CollectionId: "cities"}},
				Where: &firestorepb.StructuredQuery_Filter{
					FilterType: &firestorepb.StructuredQuery_Filter_FieldFilter{
						FieldFilter: &firestorepb.StructuredQuery_FieldFilter{
							Field: &firestorepb.StructuredQuery_FieldReference{FieldPath: "city"},
							Op:    firestorepb.StructuredQuery_FieldFilter_IN,
							Value: &firestorepb.Value{ValueType: &firestorepb.Value_ArrayValue{ArrayValue: &firestorepb.ArrayValue{
								Values: []*firestorepb.Value{
									{ValueType: &firestorepb.Value_StringValue{StringValue: "Paris"}},
									{ValueType: &firestorepb.Value_StringValue{StringValue: "Berlin"}},
								},
							}}},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	inCount := 0
	for {
		resp, err := inStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("IN Recv: %v", err)
		}
		if resp.GetDocument() != nil {
			inCount++
		}
	}
	if inCount != 2 {
		t.Fatalf("IN matches = %d", inCount)
	}

	acStream, err := client.RunQuery(ctx, &firestorepb.RunQueryRequest{
		Parent: parent,
		QueryType: &firestorepb.RunQueryRequest_StructuredQuery{
			StructuredQuery: &firestorepb.StructuredQuery{
				From: []*firestorepb.StructuredQuery_CollectionSelector{{CollectionId: "cities"}},
				Where: &firestorepb.StructuredQuery_Filter{
					FilterType: &firestorepb.StructuredQuery_Filter_FieldFilter{
						FieldFilter: &firestorepb.StructuredQuery_FieldFilter{
							Field: &firestorepb.StructuredQuery_FieldReference{FieldPath: "tags"},
							Op:    firestorepb.StructuredQuery_FieldFilter_ARRAY_CONTAINS,
							Value: &firestorepb.Value{ValueType: &firestorepb.Value_StringValue{StringValue: "eu"}},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	acCount := 0
	for {
		resp, err := acStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("ARRAY_CONTAINS Recv: %v", err)
		}
		if resp.GetDocument() != nil {
			acCount++
		}
	}
	if acCount != 2 {
		t.Fatalf("ARRAY_CONTAINS matches = %d", acCount)
	}
}

func TestBeginCommitRollbackTransactionToken(t *testing.T) {
	client, _, cleanup := startFirestore(t)
	defer cleanup()
	ctx := authCtx("test-root-token")
	db := "projects/noctaxris-gcp-local/databases/(default)"
	parent := db + "/documents"

	begun, err := client.BeginTransaction(ctx, &firestorepb.BeginTransactionRequest{Database: db})
	if err != nil {
		t.Fatal(err)
	}
	if len(begun.GetTransaction()) == 0 {
		t.Fatal("empty transaction token")
	}

	_, err = client.Commit(ctx, &firestorepb.CommitRequest{
		Database:    db,
		Transaction: begun.GetTransaction(),
		Writes: []*firestorepb.Write{{
			Operation: &firestorepb.Write_Update{
				Update: &firestorepb.Document{
					Name: parent + "/tx/doc1",
					Fields: map[string]*firestorepb.Value{
						"v": {ValueType: &firestorepb.Value_StringValue{StringValue: "one"}},
					},
				},
			},
			UpdateMask: &firestorepb.DocumentMask{FieldPaths: []string{"v"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.GetDocument(ctx, &firestorepb.GetDocumentRequest{Name: parent + "/tx/doc1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetFields()["v"].GetStringValue() != "one" {
		t.Fatalf("fields=%#v", got.GetFields())
	}

	// Token already consumed.
	_, err = client.Commit(ctx, &firestorepb.CommitRequest{
		Database: db, Transaction: begun.GetTransaction(),
	})
	if err == nil {
		t.Fatal("expected reused transaction to fail")
	}

	begun2, err := client.BeginTransaction(ctx, &firestorepb.BeginTransactionRequest{Database: db})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Rollback(ctx, &firestorepb.RollbackRequest{
		Database: db, Transaction: begun2.GetTransaction(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Commit(ctx, &firestorepb.CommitRequest{
		Database: db, Transaction: begun2.GetTransaction(),
	})
	if err == nil {
		t.Fatal("expected rolled-back transaction to fail")
	}
}
