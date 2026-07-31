package sdk_test

import (
	"testing"

	"cloud.google.com/go/datastore/apiv1/datastorepb"
	"cloud.google.com/go/firestore/apiv1/firestorepb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestFirestoreCreateGetSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	conn, err := grpc.NewClient(grpcDialTarget(ep), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc dial: %v", err)
	}
	defer conn.Close()
	client := firestorepb.NewFirestoreClient(conn)
	ctx := grpcAuthCtx(token)

	parent := "projects/" + project + "/databases/(default)/documents"
	docID := uniqueID("sdk-fs")
	created, err := client.CreateDocument(ctx, &firestorepb.CreateDocumentRequest{
		Parent:       parent,
		CollectionId: "sdk_smoke",
		DocumentId:   docID,
		Document: &firestorepb.Document{
			Fields: map[string]*firestorepb.Value{
				"ping": {ValueType: &firestorepb.Value_StringValue{StringValue: "pong"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	got, err := client.GetDocument(ctx, &firestorepb.GetDocumentRequest{Name: created.GetName()})
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	if got.GetFields()["ping"].GetStringValue() != "pong" {
		t.Fatalf("fields=%#v", got.GetFields())
	}
}

func TestDatastoreCommitLookupSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	conn, err := grpc.NewClient(grpcDialTarget(ep), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc dial: %v", err)
	}
	defer conn.Close()
	client := datastorepb.NewDatastoreClient(conn)
	ctx := grpcAuthCtx(token)

	name := uniqueID("sdk-ds")
	key := &datastorepb.Key{
		PartitionId: &datastorepb.PartitionId{ProjectId: project},
		Path: []*datastorepb.Key_PathElement{{
			Kind: "SdkSmoke", IdType: &datastorepb.Key_PathElement_Name{Name: name},
		}},
	}
	_, err = client.Commit(ctx, &datastorepb.CommitRequest{
		ProjectId: project,
		Mode:      datastorepb.CommitRequest_NON_TRANSACTIONAL,
		Mutations: []*datastorepb.Mutation{{
			Operation: &datastorepb.Mutation_Upsert{
				Upsert: &datastorepb.Entity{
					Key: key,
					Properties: map[string]*datastorepb.Value{
						"v": {ValueType: &datastorepb.Value_StringValue{StringValue: "ok"}},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	lookup, err := client.Lookup(ctx, &datastorepb.LookupRequest{ProjectId: project, Keys: []*datastorepb.Key{key}})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(lookup.Found) != 1 {
		t.Fatalf("found=%v missing=%v", lookup.Found, lookup.Missing)
	}
}
