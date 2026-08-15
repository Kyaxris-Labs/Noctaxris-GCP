package firestore_test

import (
	"testing"

	"cloud.google.com/go/firestore/apiv1/firestorepb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestBatchWriteTransformAndCompareFilters(t *testing.T) {
	client, _, cleanup := startFirestore(t)
	defer cleanup()
	parent := "projects/noctaxris-gcp-local/databases/(default)/documents"
	db := "projects/noctaxris-gcp-local/databases/(default)"
	ctx := authCtx("test-root-token")

	name := parent + "/cmp/doc1"
	_, err := client.CreateDocument(ctx, &firestorepb.CreateDocumentRequest{
		Parent: parent, CollectionId: "cmp", DocumentId: "doc1",
		Document: &firestorepb.Document{Fields: map[string]*firestorepb.Value{
			"score": {ValueType: &firestorepb.Value_IntegerValue{IntegerValue: 10}},
			"name":  {ValueType: &firestorepb.Value_StringValue{StringValue: "a"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateDocument(ctx, &firestorepb.CreateDocumentRequest{
		Parent: parent, CollectionId: "cmp", DocumentId: "doc2",
		Document: &firestorepb.Document{Fields: map[string]*firestorepb.Value{
			"score": {ValueType: &firestorepb.Value_DoubleValue{DoubleValue: 20.5}},
			"name":  {ValueType: &firestorepb.Value_StringValue{StringValue: "b"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.BatchWrite(ctx, &firestorepb.BatchWriteRequest{
		Database: db,
		Writes: []*firestorepb.Write{{
			Operation: &firestorepb.Write_Transform{
				Transform: &firestorepb.DocumentTransform{
					Document: name,
					FieldTransforms: []*firestorepb.DocumentTransform_FieldTransform{{
						FieldPath: "score",
						TransformType: &firestorepb.DocumentTransform_FieldTransform_Increment{
							Increment: &firestorepb.Value{ValueType: &firestorepb.Value_IntegerValue{IntegerValue: 3}},
						},
					}},
				},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := client.RunQuery(ctx, &firestorepb.RunQueryRequest{
		Parent: parent,
		QueryType: &firestorepb.RunQueryRequest_StructuredQuery{
			StructuredQuery: &firestorepb.StructuredQuery{
				From: []*firestorepb.StructuredQuery_CollectionSelector{{CollectionId: "cmp"}},
				Where: &firestorepb.StructuredQuery_Filter{
					FilterType: &firestorepb.StructuredQuery_Filter_FieldFilter{
						FieldFilter: &firestorepb.StructuredQuery_FieldFilter{
							Field: &firestorepb.StructuredQuery_FieldReference{FieldPath: "score"},
							Op:    firestorepb.StructuredQuery_FieldFilter_GREATER_THAN,
							Value: &firestorepb.Value{ValueType: &firestorepb.Value_IntegerValue{IntegerValue: 5}},
						},
					},
				},
				OrderBy: []*firestorepb.StructuredQuery_Order{{
					Field: &firestorepb.StructuredQuery_FieldReference{FieldPath: "score"},
					Direction: firestorepb.StructuredQuery_ASCENDING,
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for {
		msg, err := stream.Recv()
		if err != nil {
			break
		}
		if msg.GetDocument() != nil {
			n++
		}
	}
	if n < 1 {
		t.Fatalf("expected compare filter matches, got %d", n)
	}

	_, err = client.BatchWrite(ctx, &firestorepb.BatchWriteRequest{
		Database: db,
		Writes: []*firestorepb.Write{{
			Operation: &firestorepb.Write_Update{
				Update: &firestorepb.Document{
					Name: name,
					Fields: map[string]*firestorepb.Value{
						"touched": {ValueType: &firestorepb.Value_TimestampValue{TimestampValue: timestamppb.Now()}},
					},
				},
			},
			UpdateMask: &firestorepb.DocumentMask{FieldPaths: []string{"touched"}},
			UpdateTransforms: []*firestorepb.DocumentTransform_FieldTransform{{
				FieldPath: "score",
				TransformType: &firestorepb.DocumentTransform_FieldTransform_Increment{
					Increment: &firestorepb.Value{ValueType: &firestorepb.Value_DoubleValue{DoubleValue: 1.5}},
				},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
}
