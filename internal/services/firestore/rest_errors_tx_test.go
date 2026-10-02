package firestore_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cloud.google.com/go/firestore/apiv1/firestorepb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	fsvc "github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/firestore"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFirestoreRESTErrorShapesAndUnauth(t *testing.T) {
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

	mux := http.NewServeMux()
	svc := &fsvc.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.MountREST(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{}, false
	})
	req := httptest.NewRequest(http.MethodGet,
		"/v1/projects/"+project+"/databases/(default)/documents/users/a", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth: %d", rec.Code)
	}

	who := authn.Principal{Email: "root@" + project + ".iam.gserviceaccount.com", IsRoot: true}
	mux2 := http.NewServeMux()
	svc2 := &fsvc.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc2.MountREST(mux2, func(*http.Request) (authn.Principal, bool) { return who, true })

	req = httptest.NewRequest(http.MethodPost,
		"/v1/projects/"+project+"/databases/other/documents/users",
		strings.NewReader(`{"fields":{}}`))
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad database: %d %s", rec.Code, rec.Body.String())
	}

	body := `{"fields":{"n":{"integerValue":"1"}}}`
	req = httptest.NewRequest(http.MethodPost,
		"/v1/projects/"+project+"/databases/(default)/documents/notes?documentId=n1",
		strings.NewReader(body))
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost,
		"/v1/projects/"+project+"/databases/(default)/documents/notes?documentId=n1",
		strings.NewReader(body))
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup create: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet,
		"/v1/projects/"+project+"/databases/(default)/documents/notes/missing", nil)
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing get: %d", rec.Code)
	}

	denyWho := authn.Principal{Email: "nobody@example.com", IsRoot: false}
	mux3 := http.NewServeMux()
	svc3 := &fsvc.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc3.MountREST(mux3, func(*http.Request) (authn.Principal, bool) { return denyWho, true })
	req = httptest.NewRequest(http.MethodGet,
		"/v1/projects/"+project+"/databases/(default)/documents/notes/n1", nil)
	rec = httptest.NewRecorder()
	mux3.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("deny get: %d %s", rec.Code, rec.Body.String())
	}
}

func TestFirestoreBeginRollbackTransaction(t *testing.T) {
	client, _, cleanup := startFirestore(t)
	defer cleanup()
	ctx := authCtx("test-root-token")
	db := "projects/noctaxris-gcp-local/databases/(default)"

	_, err := client.BeginTransaction(ctx, &firestorepb.BeginTransactionRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty db: %v", err)
	}
	begun, err := client.BeginTransaction(ctx, &firestorepb.BeginTransactionRequest{Database: db})
	if err != nil {
		t.Fatal(err)
	}
	if len(begun.GetTransaction()) == 0 {
		t.Fatal("empty transaction token")
	}

	_, err = client.Rollback(ctx, &firestorepb.RollbackRequest{Database: db})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing tx: %v", err)
	}
	_, err = client.Rollback(ctx, &firestorepb.RollbackRequest{
		Database: db, Transaction: begun.GetTransaction(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Rollback(ctx, &firestorepb.RollbackRequest{
		Database: db, Transaction: begun.GetTransaction(),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("second rollback: %v", err)
	}

	_, err = client.BeginTransaction(context.Background(), &firestorepb.BeginTransactionRequest{Database: db})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauth begin: %v", err)
	}
}
