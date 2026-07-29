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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
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

func TestRunQueryCollectionGroupOrderLimitInequality(t *testing.T) {
	client, _, cleanup := startFirestore(t)
	defer cleanup()
	parent := "projects/noctaxris-gcp-local/databases/(default)/documents"
	ctx := authCtx("test-root-token")

	// Nested under different parents with same collection id "items".
	for _, pair := range []struct {
		parentDoc, id string
		score         int64
	}{
		{"rooms/r1", "a", 10},
		{"rooms/r1", "b", 30},
		{"rooms/r2", "c", 20},
	} {
		p := parent + "/" + pair.parentDoc
		if _, err := client.CreateDocument(ctx, &firestorepb.CreateDocumentRequest{
			Parent: p, CollectionId: "items", DocumentId: pair.id,
			Document: &firestorepb.Document{Fields: map[string]*firestorepb.Value{
				"score": {ValueType: &firestorepb.Value_IntegerValue{IntegerValue: pair.score}},
			}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	stream, err := client.RunQuery(ctx, &firestorepb.RunQueryRequest{
		Parent: parent,
		QueryType: &firestorepb.RunQueryRequest_StructuredQuery{
			StructuredQuery: &firestorepb.StructuredQuery{
				From: []*firestorepb.StructuredQuery_CollectionSelector{{
					CollectionId: "items", AllDescendants: true,
				}},
				Where: &firestorepb.StructuredQuery_Filter{
					FilterType: &firestorepb.StructuredQuery_Filter_FieldFilter{
						FieldFilter: &firestorepb.StructuredQuery_FieldFilter{
							Field: &firestorepb.StructuredQuery_FieldReference{FieldPath: "score"},
							Op:    firestorepb.StructuredQuery_FieldFilter_GREATER_THAN,
							Value: &firestorepb.Value{ValueType: &firestorepb.Value_IntegerValue{IntegerValue: 10}},
						},
					},
				},
				OrderBy: []*firestorepb.StructuredQuery_Order{{
					Field:     &firestorepb.StructuredQuery_FieldReference{FieldPath: "score"},
					Direction: firestorepb.StructuredQuery_ASCENDING,
				}},
				Limit: wrapperspb.Int32(2),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var scores []int64
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if d := resp.GetDocument(); d != nil {
			scores = append(scores, d.GetFields()["score"].GetIntegerValue())
		}
	}
	if len(scores) != 2 || scores[0] != 20 || scores[1] != 30 {
		t.Fatalf("scores=%v", scores)
	}
}

func TestCommitFieldTransformAndPartitionQuery(t *testing.T) {
	client, _, cleanup := startFirestore(t)
	defer cleanup()
	ctx := authCtx("test-root-token")
	db := "projects/noctaxris-gcp-local/databases/(default)"
	parent := db + "/documents"
	name := parent + "/counters/c1"

	_, err := client.CreateDocument(ctx, &firestorepb.CreateDocumentRequest{
		Parent: parent, CollectionId: "counters", DocumentId: "c1",
		Document: &firestorepb.Document{Fields: map[string]*firestorepb.Value{
			"n": {ValueType: &firestorepb.Value_IntegerValue{IntegerValue: 5}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Commit(ctx, &firestorepb.CommitRequest{
		Database: db,
		Writes: []*firestorepb.Write{{
			Operation: &firestorepb.Write_Transform{
				Transform: &firestorepb.DocumentTransform{
					Document: name,
					FieldTransforms: []*firestorepb.DocumentTransform_FieldTransform{
						{
							FieldPath: "n",
							TransformType: &firestorepb.DocumentTransform_FieldTransform_Increment{
								Increment: &firestorepb.Value{ValueType: &firestorepb.Value_IntegerValue{IntegerValue: 2}},
							},
						},
						{
							FieldPath: "updated",
							TransformType: &firestorepb.DocumentTransform_FieldTransform_SetToServerValue{
								SetToServerValue: firestorepb.DocumentTransform_FieldTransform_REQUEST_TIME,
							},
						},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.GetDocument(ctx, &firestorepb.GetDocumentRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetFields()["n"].GetIntegerValue() != 7 {
		t.Fatalf("n=%v", got.GetFields()["n"])
	}
	if got.GetFields()["updated"].GetTimestampValue() == nil {
		t.Fatalf("expected server timestamp, fields=%#v", got.GetFields())
	}

	part, err := client.PartitionQuery(ctx, &firestorepb.PartitionQueryRequest{
		Parent:         parent,
		PartitionCount: 8,
		QueryType: &firestorepb.PartitionQueryRequest_StructuredQuery{
			StructuredQuery: &firestorepb.StructuredQuery{
				From: []*firestorepb.StructuredQuery_CollectionSelector{{
					CollectionId: "counters", AllDescendants: true,
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(part.GetPartitions()) != 1 {
		t.Fatalf("partitions=%d", len(part.GetPartitions()))
	}
}


func TestCommitAtomicWithPreconditions(t *testing.T) {
	client, _, cleanup := startFirestore(t)
	defer cleanup()
	ctx := authCtx("test-root-token")
	db := "projects/noctaxris-gcp-local/databases/(default)"
	parent := db + "/documents"

	begun, err := client.BeginTransaction(ctx, &firestorepb.BeginTransactionRequest{Database: db})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Commit(ctx, &firestorepb.CommitRequest{
		Database:    db,
		Transaction: begun.GetTransaction(),
		Writes: []*firestorepb.Write{
			{
				Operation: &firestorepb.Write_Update{
					Update: &firestorepb.Document{
						Name: parent + "/acct/a",
						Fields: map[string]*firestorepb.Value{
							"bal": {ValueType: &firestorepb.Value_IntegerValue{IntegerValue: 10}},
						},
					},
				},
				UpdateMask:       &firestorepb.DocumentMask{FieldPaths: []string{"bal"}},
				CurrentDocument: &firestorepb.Precondition{ConditionType: &firestorepb.Precondition_Exists{Exists: false}},
			},
			{
				Operation: &firestorepb.Write_Update{
					Update: &firestorepb.Document{
						Name: parent + "/acct/b",
						Fields: map[string]*firestorepb.Value{
							"bal": {ValueType: &firestorepb.Value_IntegerValue{IntegerValue: 20}},
						},
					},
				},
				UpdateMask:       &firestorepb.DocumentMask{FieldPaths: []string{"bal"}},
				CurrentDocument: &firestorepb.Precondition{ConditionType: &firestorepb.Precondition_Exists{Exists: false}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Second create of acct/a must fail with AlreadyExists precondition.
	begun2, err := client.BeginTransaction(ctx, &firestorepb.BeginTransactionRequest{Database: db})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Commit(ctx, &firestorepb.CommitRequest{
		Database:    db,
		Transaction: begun2.GetTransaction(),
		Writes: []*firestorepb.Write{{
			Operation: &firestorepb.Write_Update{
				Update: &firestorepb.Document{
					Name: parent + "/acct/a",
					Fields: map[string]*firestorepb.Value{
						"bal": {ValueType: &firestorepb.Value_IntegerValue{IntegerValue: 99}},
					},
				},
			},
			UpdateMask:       &firestorepb.DocumentMask{FieldPaths: []string{"bal"}},
			CurrentDocument: &firestorepb.Precondition{ConditionType: &firestorepb.Precondition_Exists{Exists: false}},
		}},
	})
	if err == nil {
		t.Fatal("expected AlreadyExists precondition failure")
	}
	got, err := client.GetDocument(ctx, &firestorepb.GetDocumentRequest{Name: parent + "/acct/a"})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetFields()["bal"].GetIntegerValue() != 10 {
		t.Fatalf("atomic rollback expected bal=10, got %#v", got.GetFields())
	}
}

func TestBatchWriteMultiple(t *testing.T) {
	client, _, cleanup := startFirestore(t)
	defer cleanup()
	ctx := authCtx("test-root-token")
	db := "projects/noctaxris-gcp-local/databases/(default)"
	parent := db + "/documents"
	resp, err := client.BatchWrite(ctx, &firestorepb.BatchWriteRequest{
		Database: db,
		Writes: []*firestorepb.Write{
			{
				Operation: &firestorepb.Write_Update{
					Update: &firestorepb.Document{
						Name: parent + "/batch/d1",
						Fields: map[string]*firestorepb.Value{
							"x": {ValueType: &firestorepb.Value_StringValue{StringValue: "one"}},
						},
					},
				},
				UpdateMask: &firestorepb.DocumentMask{FieldPaths: []string{"x"}},
			},
			{
				Operation: &firestorepb.Write_Update{
					Update: &firestorepb.Document{
						Name: parent + "/batch/d2",
						Fields: map[string]*firestorepb.Value{
							"x": {ValueType: &firestorepb.Value_StringValue{StringValue: "two"}},
						},
					},
				},
				UpdateMask: &firestorepb.DocumentMask{FieldPaths: []string{"x"}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetWriteResults()) != 2 {
		t.Fatalf("writeResults=%d", len(resp.GetWriteResults()))
	}
	got, err := client.GetDocument(ctx, &firestorepb.GetDocumentRequest{Name: parent + "/batch/d2"})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetFields()["x"].GetStringValue() != "two" {
		t.Fatalf("fields=%#v", got.GetFields())
	}
}

func TestBatchWriteDelete(t *testing.T) {
	client, _, cleanup := startFirestore(t)
	defer cleanup()
	ctx := authCtx("test-root-token")
	db := "projects/noctaxris-gcp-local/databases/(default)"
	parent := db + "/documents"
	name := parent + "/batch/del"

	_, err := client.CreateDocument(ctx, &firestorepb.CreateDocumentRequest{
		Parent: parent, CollectionId: "batch", DocumentId: "del",
		Document: &firestorepb.Document{Fields: map[string]*firestorepb.Value{
			"x": {ValueType: &firestorepb.Value_StringValue{StringValue: "gone"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.BatchWrite(ctx, &firestorepb.BatchWriteRequest{
		Database: db,
		Writes: []*firestorepb.Write{{
			Operation: &firestorepb.Write_Delete{Delete: name},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetStatus()) != 1 || resp.GetStatus()[0].GetCode() != int32(0) {
		t.Fatalf("status=%v", resp.GetStatus())
	}
	_, err = client.GetDocument(ctx, &firestorepb.GetDocumentRequest{Name: name})
	if err == nil {
		t.Fatal("expected NotFound after BatchWrite delete")
	}
}

func TestFirestoreAuthzDenyNonRoot(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	project := "noctaxris-gcp-local"
	if err := st.EnsureRoot(project, "root@"+project+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	svc := &fsvc.Service{
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
	firestorepb.RegisterFirestoreServer(gs, svc)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(func() { gs.Stop() })
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := firestorepb.NewFirestoreClient(conn)
	_, err = client.CreateDocument(context.Background(), &firestorepb.CreateDocumentRequest{
		Parent:       "projects/" + project + "/databases/(default)/documents",
		CollectionId: "deny",
		DocumentId:   "doc1",
		Document: &firestorepb.Document{
			Fields: map[string]*firestorepb.Value{
				"x": {ValueType: &firestorepb.Value_StringValue{StringValue: "no"}},
			},
		},
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", err)
	}
}
