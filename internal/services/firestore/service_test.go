package firestore_test

import (
	"context"
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
