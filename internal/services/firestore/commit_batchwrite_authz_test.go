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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func TestCommitBatchWriteRequireEntityPerms(t *testing.T) {
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
	viewer := "viewer@" + project + ".iam.gserviceaccount.com"
	user := "ds-user@" + project + ".iam.gserviceaccount.com"
	if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Bindings: []authz.Binding{
			{Role: "roles/datastore.viewer", Members: []string{"serviceAccount:" + viewer}},
			{Role: "roles/datastore.user", Members: []string{"serviceAccount:" + user}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	principal := viewer
	svc := &fsvc.Service{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		PrincipalFrom: func(context.Context) (authn.Principal, bool) {
			return authn.Principal{Email: principal, IsRoot: false}, true
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
	db := "projects/" + project + "/databases/(default)"
	doc := db + "/documents/notes/n1"
	update := &firestorepb.Write{
		Operation: &firestorepb.Write_Update{Update: &firestorepb.Document{
			Name: doc,
			Fields: map[string]*firestorepb.Value{
				"v": {ValueType: &firestorepb.Value_StringValue{StringValue: "1"}},
			},
		}},
	}
	del := &firestorepb.Write{Operation: &firestorepb.Write_Delete{Delete: doc}}

	_, err = client.Commit(context.Background(), &firestorepb.CommitRequest{
		Database: db, Writes: []*firestorepb.Write{update},
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("viewer Commit want PermissionDenied got %v", err)
	}
	_, err = client.BatchWrite(context.Background(), &firestorepb.BatchWriteRequest{
		Database: db, Writes: []*firestorepb.Write{del},
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("viewer BatchWrite delete want PermissionDenied got %v", err)
	}

	principal = user
	_, err = client.Commit(context.Background(), &firestorepb.CommitRequest{
		Database: db, Writes: []*firestorepb.Write{update},
	})
	if err != nil {
		t.Fatalf("datastore.user Commit: %v", err)
	}
	_, err = client.BatchWrite(context.Background(), &firestorepb.BatchWriteRequest{
		Database: db, Writes: []*firestorepb.Write{del},
	})
	if err != nil {
		t.Fatalf("datastore.user BatchWrite delete: %v", err)
	}
}
