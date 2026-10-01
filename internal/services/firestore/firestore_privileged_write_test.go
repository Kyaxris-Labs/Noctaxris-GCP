package firestore_test

import (
	"testing"

	"cloud.google.com/go/firestore/apiv1/firestorepb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func toolkitJWT(uid string) string {
	tok, err := authn.MintIdentityToolkitIDToken("noctaxris-gcp-local", uid, uid+"@example.com", nil)
	if err != nil {
		panic(err)
	}
	return tok
}

func TestFirestoreOwnUsersDocumentPrivilegedWrite(t *testing.T) {
	client, _, cleanup := startFirestore(t)
	defer cleanup()
	parent := "projects/noctaxris-gcp-local/databases/(default)/documents"

	own, err := client.CreateDocument(authCtx(toolkitJWT("uid-own")), &firestorepb.CreateDocumentRequest{
		Parent:       parent,
		CollectionId: "users",
		DocumentId:   "uid-own",
		Document: &firestorepb.Document{
			Fields: map[string]*firestorepb.Value{
				"role": {ValueType: &firestorepb.Value_StringValue{StringValue: "admin"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("own write: %v", err)
	}
	if own.GetFields()["role"].GetStringValue() != "admin" {
		t.Fatalf("own doc %#v", own)
	}

	_, err = client.CreateDocument(authCtx(toolkitJWT("uid-own")), &firestorepb.CreateDocumentRequest{
		Parent:       parent,
		CollectionId: "users",
		DocumentId:   "uid-other",
		Document: &firestorepb.Document{
			Fields: map[string]*firestorepb.Value{
				"role": {ValueType: &firestorepb.Value_StringValue{StringValue: "admin"}},
			},
		},
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("other user doc want PermissionDenied got %v", err)
	}
}
