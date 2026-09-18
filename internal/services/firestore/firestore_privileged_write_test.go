package firestore_test

import (
	"encoding/base64"
	"testing"

	"cloud.google.com/go/firestore/apiv1/firestorepb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func toolkitJWT(uid string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"https://securetoken.google.com/noctaxris-gcp-local","user_id":"` + uid + `","sub":"` + uid + `"}`))
	return header + "." + payload + "."
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
