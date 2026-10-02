package firestore_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"cloud.google.com/go/firestore/apiv1/firestorepb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	fsvc "github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/firestore"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFirestoreRESTOwnerReadAllowAndDeny(t *testing.T) {
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
	rootEmail := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, rootEmail); err != nil {
		t.Fatal(err)
	}

	base := "projects/" + project + "/databases/(default)/documents/"
	for _, id := range []string{"uid-own", "uid-other"} {
		d := store.FirestoreDoc{
			Path: base + "users/" + id, ProjectID: project, CollectionID: "users", DocumentID: id,
			FieldsJSON: `{"role":{"stringValue":"` + id + `"}}`,
			CreateTime: "2026-01-01T00:00:00Z", UpdateTime: "2026-01-01T00:00:00Z",
		}
		if err := st.PutFirestoreDoc(d); err != nil {
			t.Fatal(err)
		}
	}

	var who authn.Principal
	mux := http.NewServeMux()
	svc := &fsvc.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.MountREST(mux, func(*http.Request) (authn.Principal, bool) { return who, true })

	get := func(docPath string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet,
			"/v1/projects/"+project+"/databases/(default)/documents/"+docPath, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	who = authn.Principal{Email: "uid-own", IsRoot: false}
	rec := get("users/uid-own")
	if rec.Code != http.StatusOK {
		t.Fatalf("owner get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["name"] != base+"users/uid-own" {
		t.Fatalf("name %#v", doc["name"])
	}
	fields, _ := doc["fields"].(map[string]any)
	if fields == nil || fields["role"] == nil {
		t.Fatalf("fields %#v", doc["fields"])
	}
	if doc["createTime"] == nil || doc["updateTime"] == nil {
		t.Fatalf("times missing %#v", doc)
	}

	if rec := get("users/uid-other"); rec.Code != http.StatusForbidden {
		t.Fatalf("other uid get status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := get("secrets/nested/documents/users/uid-own"); rec.Code != http.StatusForbidden {
		t.Fatalf("suffix bypass get status=%d body=%s", rec.Code, rec.Body.String())
	}

	who = authn.Principal{Email: "user:uid-missing", IsRoot: false}
	if rec := get("users/uid-missing"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing own doc status=%d body=%s", rec.Code, rec.Body.String())
	}

	who = authn.Principal{Email: rootEmail, IsRoot: true}
	for _, id := range []string{"uid-own", "uid-other"} {
		if rec := get("users/" + id); rec.Code != http.StatusOK {
			t.Fatalf("root get %s status=%d body=%s", id, rec.Code, rec.Body.String())
		}
	}
	if rec := get("users/uid-nobody"); rec.Code != http.StatusNotFound {
		t.Fatalf("root missing status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestFirestoreOwnUsersDocumentGRPCRead(t *testing.T) {
	client, _, cleanup := startFirestore(t)
	defer cleanup()
	parent := "projects/noctaxris-gcp-local/databases/(default)/documents"
	for _, id := range []string{"uid-own", "uid-other"} {
		_, err := client.CreateDocument(authCtx(toolkitJWT(id)), &firestorepb.CreateDocumentRequest{
			Parent:       parent,
			CollectionId: "users",
			DocumentId:   id,
			Document: &firestorepb.Document{
				Fields: map[string]*firestorepb.Value{
					"role": {ValueType: &firestorepb.Value_StringValue{StringValue: "user"}},
				},
			},
		})
		if err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	got, err := client.GetDocument(authCtx(toolkitJWT("uid-own")), &firestorepb.GetDocumentRequest{Name: parent + "/users/uid-own"})
	if err != nil {
		t.Fatalf("own read: %v", err)
	}
	if got.GetFields()["role"].GetStringValue() != "user" {
		t.Fatalf("own doc %#v", got)
	}

	_, err = client.GetDocument(authCtx(toolkitJWT("uid-own")), &firestorepb.GetDocumentRequest{Name: parent + "/users/uid-other"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("other user read want PermissionDenied got %v", err)
	}
}
