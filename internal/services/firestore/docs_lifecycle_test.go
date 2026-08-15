package firestore_test

import (
	"io"
	"testing"

	"cloud.google.com/go/firestore/apiv1/firestorepb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestListDeleteBatchGetAndAutoID(t *testing.T) {
	client, _, cleanup := startFirestore(t)
	defer cleanup()
	parent := "projects/noctaxris-gcp-local/databases/(default)/documents"
	ctx := authCtx("test-root-token")

	created, err := client.CreateDocument(ctx, &firestorepb.CreateDocumentRequest{
		Parent:       parent,
		CollectionId: "life",
		Document: &firestorepb.Document{
			Fields: map[string]*firestorepb.Value{
				"n": {ValueType: &firestorepb.Value_IntegerValue{IntegerValue: 1}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.GetName() == "" {
		t.Fatal("expected auto-generated document name")
	}

	_, err = client.CreateDocument(ctx, &firestorepb.CreateDocumentRequest{
		Parent: parent, CollectionId: "life", DocumentId: "keep",
		Document: &firestorepb.Document{
			Fields: map[string]*firestorepb.Value{
				"n": {ValueType: &firestorepb.Value_IntegerValue{IntegerValue: 2}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	listed, err := client.ListDocuments(ctx, &firestorepb.ListDocumentsRequest{
		Parent: parent, CollectionId: "life", PageSize: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.GetDocuments()) < 2 {
		t.Fatalf("list=%v", listed.GetDocuments())
	}
	_, err = client.ListDocuments(ctx, &firestorepb.ListDocumentsRequest{Parent: parent})
	if err == nil {
		t.Fatal("expected invalid argument without collection_id")
	}
	if st, ok := status.FromError(err); !ok || st.Code() != codes.InvalidArgument {
		t.Fatalf("list err=%v", err)
	}

	stream, err := client.BatchGetDocuments(ctx, &firestorepb.BatchGetDocumentsRequest{
		Database:  "projects/noctaxris-gcp-local/databases/(default)",
		Documents: []string{created.GetName(), parent + "/life/keep", parent + "/life/missing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	found, missing := 0, 0
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch msg.Result.(type) {
		case *firestorepb.BatchGetDocumentsResponse_Found:
			found++
		case *firestorepb.BatchGetDocumentsResponse_Missing:
			missing++
		}
	}
	if found != 2 || missing != 1 {
		t.Fatalf("batch get found=%d missing=%d", found, missing)
	}
	emptyStream, err := client.BatchGetDocuments(ctx, &firestorepb.BatchGetDocumentsRequest{})
	if err == nil {
		_, err = emptyStream.Recv()
	}
	if err == nil {
		t.Fatal("expected empty documents invalid")
	}

	_, err = client.DeleteDocument(ctx, &firestorepb.DeleteDocumentRequest{Name: created.GetName()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.DeleteDocument(ctx, &firestorepb.DeleteDocumentRequest{Name: created.GetName()})
	if err == nil {
		t.Fatal("expected not found on second delete")
	}
	if st, ok := status.FromError(err); !ok || st.Code() != codes.NotFound {
		t.Fatalf("delete err=%v", err)
	}
	_, err = client.DeleteDocument(ctx, &firestorepb.DeleteDocumentRequest{})
	if err == nil {
		t.Fatal("expected invalid argument")
	}
}
