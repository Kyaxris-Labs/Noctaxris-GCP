package firestore_test

import (
	"testing"
	"time"

	"cloud.google.com/go/firestore/apiv1/firestorepb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestBatchGetCommitBatchWriteBindDocumentProject(t *testing.T) {
	client, st, cleanup := startFirestore(t)
	defer cleanup()

	const local = "noctaxris-gcp-local"
	const other = "other-project"
	if err := st.EnsureRoot(other, "root@"+other+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	otherPath := "projects/" + other + "/databases/(default)/documents/escape/doc1"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := st.PutFirestoreDoc(store.FirestoreDoc{
		Path: otherPath, ProjectID: other, CollectionID: "escape", DocumentID: "doc1",
		FieldsJSON: `{"x":{"stringValue":"leak"}}`, CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatal(err)
	}

	ctx := authCtx("test-root-token")
	db := "projects/" + local + "/databases/(default)"
	localPath := db + "/documents/users/ada"

	_, err := client.CreateDocument(ctx, &firestorepb.CreateDocumentRequest{
		Parent:       db + "/documents",
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

	// Positive: BatchGet same-project paths.
	stream, err := client.BatchGetDocuments(ctx, &firestorepb.BatchGetDocumentsRequest{
		Database:  db,
		Documents: []string{localPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetFound() == nil || resp.GetFound().GetName() != localPath {
		t.Fatalf("BatchGet same-project got %#v", resp)
	}

	// Negative: BatchGet cross-project path after authorizing on local database.
	stream, err = client.BatchGetDocuments(ctx, &firestorepb.BatchGetDocumentsRequest{
		Database:  db,
		Documents: []string{localPath, otherPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = stream.Recv()
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("BatchGet cross-project code=%v err=%v", status.Code(err), err)
	}

	// Positive: Commit same-project write.
	_, err = client.Commit(ctx, &firestorepb.CommitRequest{
		Database: db,
		Writes: []*firestorepb.Write{{
			Operation: &firestorepb.Write_Update{
				Update: &firestorepb.Document{
					Name: localPath,
					Fields: map[string]*firestorepb.Value{
						"name": {ValueType: &firestorepb.Value_StringValue{StringValue: "Ada2"}},
					},
				},
			},
			UpdateMask: &firestorepb.DocumentMask{FieldPaths: []string{"name"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Negative: Commit cross-project write path.
	_, err = client.Commit(ctx, &firestorepb.CommitRequest{
		Database: db,
		Writes: []*firestorepb.Write{{
			Operation: &firestorepb.Write_Update{
				Update: &firestorepb.Document{
					Name: otherPath,
					Fields: map[string]*firestorepb.Value{
						"x": {ValueType: &firestorepb.Value_StringValue{StringValue: "nope"}},
					},
				},
			},
			UpdateMask: &firestorepb.DocumentMask{FieldPaths: []string{"x"}},
		}},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Commit cross-project code=%v err=%v", status.Code(err), err)
	}

	// Positive: BatchWrite same-project.
	_, err = client.BatchWrite(ctx, &firestorepb.BatchWriteRequest{
		Database: db,
		Writes: []*firestorepb.Write{{
			Operation: &firestorepb.Write_Update{
				Update: &firestorepb.Document{
					Name: db + "/documents/batch/ok",
					Fields: map[string]*firestorepb.Value{
						"ok": {ValueType: &firestorepb.Value_BooleanValue{BooleanValue: true}},
					},
				},
			},
			UpdateMask: &firestorepb.DocumentMask{FieldPaths: []string{"ok"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Negative: BatchWrite cross-project.
	_, err = client.BatchWrite(ctx, &firestorepb.BatchWriteRequest{
		Database: db,
		Writes: []*firestorepb.Write{{
			Operation: &firestorepb.Write_Delete{Delete: otherPath},
		}},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("BatchWrite cross-project code=%v err=%v", status.Code(err), err)
	}
}
