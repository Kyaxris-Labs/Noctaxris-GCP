package secretmanager_test

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/secretmanager"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func startSecretManagerGRPC(t *testing.T) (secretmanagerpb.SecretManagerServiceClient, context.Context, func()) {
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
	root := "root@" + project + ".iam.gserviceaccount.com"
	token := "test-root-token"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	svc := &secretmanager.Service{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		GRPCPrincipal: func(ctx context.Context) (authn.Principal, error) {
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				return authn.Principal{}, authn.ErrUnauthenticated
			}
			vals := md.Get("authorization")
			if len(vals) == 0 || vals[0] != "Bearer "+token {
				return authn.Principal{}, authn.ErrUnauthenticated
			}
			return authn.Principal{Email: root, IsRoot: true}, nil
		},
		DefaultProject: project,
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
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	return secretmanagerpb.NewSecretManagerServiceClient(conn), ctx, cleanup
}

func TestSecretManagerGRPCLifecycle(t *testing.T) {
	client, ctx, cleanup := startSecretManagerGRPC(t)
	defer cleanup()
	project := "noctaxris-gcp-local"
	parent := "projects/" + project

	sec, err := client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{
		Parent:   parent,
		SecretId: "grpc-sec",
		Secret:   &secretmanagerpb.Secret{Labels: map[string]string{"k": "v"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	name := sec.GetName()
	got, err := client.GetSecret(ctx, &secretmanagerpb.GetSecretRequest{Name: name})
	if err != nil || got.GetName() != name {
		t.Fatalf("get: %v err=%v", got, err)
	}
	listed, err := client.ListSecrets(ctx, &secretmanagerpb.ListSecretsRequest{Parent: parent})
	if err != nil || len(listed.GetSecrets()) < 1 {
		t.Fatalf("list: %v err=%v", listed, err)
	}
	updated, err := client.UpdateSecret(ctx, &secretmanagerpb.UpdateSecretRequest{
		Secret:     &secretmanagerpb.Secret{Name: name, Labels: map[string]string{"k": "2"}},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"labels"}},
	})
	if err != nil || updated.GetLabels()["k"] != "2" {
		t.Fatalf("update: %v err=%v", updated, err)
	}

	ver, err := client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{
		Parent:  name,
		Payload: &secretmanagerpb.SecretPayload{Data: []byte("secret-bytes")},
	})
	if err != nil {
		t.Fatal(err)
	}
	access, err := client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{Name: ver.GetName()})
	if err != nil || string(access.GetPayload().GetData()) != "secret-bytes" {
		t.Fatalf("access: %v err=%v", access, err)
	}
	versions, err := client.ListSecretVersions(ctx, &secretmanagerpb.ListSecretVersionsRequest{Parent: name})
	if err != nil || len(versions.GetVersions()) < 1 {
		t.Fatalf("list versions: %v err=%v", versions, err)
	}
	_, err = client.DisableSecretVersion(ctx, &secretmanagerpb.DisableSecretVersionRequest{Name: ver.GetName()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.EnableSecretVersion(ctx, &secretmanagerpb.EnableSecretVersionRequest{Name: ver.GetName()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.DestroySecretVersion(ctx, &secretmanagerpb.DestroySecretVersionRequest{Name: ver.GetName()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.DeleteSecret(ctx, &secretmanagerpb.DeleteSecretRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}
}
