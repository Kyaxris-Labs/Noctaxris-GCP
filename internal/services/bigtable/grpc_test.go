package bigtable_test

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"cloud.google.com/go/bigtable/admin/apiv2/adminpb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	btsvc "github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/bigtable"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func startBigtableAdminGRPC(t *testing.T) (adminpb.BigtableInstanceAdminClient, *store.Store, func()) {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
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
	authnSvc := &authn.Authenticator{RootServiceAccount: rootSA, RootAccessToken: "test-root-token"}
	svc := &btsvc.Service{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		GRPCPrincipal: func(ctx context.Context) (authn.Principal, error) {
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				return authn.Principal{}, authn.ErrUnauthenticated
			}
			vals := md.Get("authorization")
			if len(vals) == 0 {
				return authn.Principal{}, authn.ErrUnauthenticated
			}
			raw := vals[0]
			const prefix = "Bearer "
			if len(raw) < len(prefix) || raw[:len(prefix)] != prefix {
				return authn.Principal{}, authn.ErrUnauthenticated
			}
			return authnSvc.AuthenticateToken(raw[len(prefix):])
		},
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	svc.RegisterGRPC(gs)
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
	return adminpb.NewBigtableInstanceAdminClient(conn), st, cleanup
}

func btAuthCtx(token string) context.Context {
	return metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
}

func TestBigtableInstanceAdminGRPCCRUD(t *testing.T) {
	client, _, cleanup := startBigtableAdminGRPC(t)
	defer cleanup()

	ctx := btAuthCtx("test-root-token")
	parent := "projects/noctaxris-gcp-local"
	name := parent + "/instances/lab"

	op, err := client.CreateInstance(ctx, &adminpb.CreateInstanceRequest{
		Parent:     parent,
		InstanceId: "lab",
		Instance: &adminpb.Instance{
			DisplayName: "Lab",
			Type:        adminpb.Instance_PRODUCTION,
			Labels:      map[string]string{"env": "test"},
		},
		Clusters: map[string]*adminpb.Cluster{
			"c1": {Location: "projects/noctaxris-gcp-local/locations/us-central1-b", ServeNodes: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !op.GetDone() {
		t.Fatalf("expected done Operation, got %#v", op)
	}
	var created adminpb.Instance
	if err := op.GetResponse().UnmarshalTo(&created); err != nil {
		t.Fatalf("unpack response: %v", err)
	}
	if created.GetName() != name || created.GetState() != adminpb.Instance_READY {
		t.Fatalf("created=%#v", &created)
	}

	got, err := client.GetInstance(ctx, &adminpb.GetInstanceRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetDisplayName() != "Lab" || got.GetLabels()["env"] != "test" {
		t.Fatalf("get=%#v", got)
	}

	list, err := client.ListInstances(ctx, &adminpb.ListInstancesRequest{Parent: parent})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.GetInstances()) != 1 || list.GetInstances()[0].GetName() != name {
		t.Fatalf("list=%#v", list)
	}

	if _, err := client.DeleteInstance(ctx, &adminpb.DeleteInstanceRequest{Name: name}); err != nil {
		t.Fatal(err)
	}
	_, err = client.GetInstance(ctx, &adminpb.GetInstanceRequest{Name: name})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound after delete, got %v", err)
	}
}

func TestBigtableInstanceAdminGRPCAuthzFailClosed(t *testing.T) {
	client, _, cleanup := startBigtableAdminGRPC(t)
	defer cleanup()

	ctx := btAuthCtx("not-a-valid-token")
	_, err := client.ListInstances(ctx, &adminpb.ListInstancesRequest{
		Parent: "projects/noctaxris-gcp-local",
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", err)
	}
}
