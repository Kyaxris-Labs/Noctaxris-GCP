package bigtable_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"cloud.google.com/go/bigtable/admin/apiv2/adminpb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/bigtable"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestBigtableRESTMissingAndInvalid(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc := &bigtable.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	base := "/v2/projects/noctaxris-gcp-local/instances"
	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(`not-json`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(`{"instance":{}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing instanceId: %d", rec.Code)
	}

	body := `{"instanceId":"dev1","instance":{"displayName":"Dev","type":"DEVELOPMENT"},"clusters":{"c1":{"location":"us-central1-b"}}}`
	req = httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create dev: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup instance: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, base+"/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing: %d", rec.Code)
	}

	tbl := base + "/dev1/tables"
	req = httptest.NewRequest(http.MethodPost, tbl, bytes.NewReader([]byte(`{"tableId":"t1","table":{"columnFamilies":{"cf":{}}}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create table: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, tbl+"/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing table: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, tbl+"/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing table: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, base+"/missing/tables",
		bytes.NewReader([]byte(`{"tableId":"x","table":{}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("create table on missing instance should fail")
	}
}

func TestBigtableGRPCDevelopmentAndMissing(t *testing.T) {
	client, st, cleanup := startBigtableAdminGRPC(t)
	defer cleanup()
	ctx := btAuthCtx("test-root-token")
	parent := "projects/noctaxris-gcp-local"

	op, err := client.CreateInstance(ctx, &adminpb.CreateInstanceRequest{
		Parent:     parent,
		InstanceId: "devgrpc",
		Instance: &adminpb.Instance{
			DisplayName: "Dev",
			Type:        adminpb.Instance_DEVELOPMENT,
		},
		Clusters: map[string]*adminpb.Cluster{
			"c1": {Location: parent + "/locations/us-central1-b", ServeNodes: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !op.GetDone() {
		t.Fatal("expected done")
	}
	var created adminpb.Instance
	if err := op.GetResponse().UnmarshalTo(&created); err != nil {
		t.Fatal(err)
	}
	if created.GetType() != adminpb.Instance_DEVELOPMENT {
		t.Fatalf("type=%v", created.GetType())
	}

	_, err = client.CreateInstance(ctx, &adminpb.CreateInstanceRequest{
		Parent: parent, InstanceId: "devgrpc", Instance: &adminpb.Instance{},
	})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("dup want AlreadyExists, got %v", err)
	}

	_, err = client.GetInstance(ctx, &adminpb.GetInstanceRequest{Name: parent + "/instances/nope"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("get missing: %v", err)
	}
	_, err = client.DeleteInstance(ctx, &adminpb.DeleteInstanceRequest{Name: parent + "/instances/nope"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("delete missing: %v", err)
	}

	// Exercise CREATING state mapping via direct store write + Get.
	name := parent + "/instances/creating"
	ok, err := st.CreateBigtableInstance(store.BigtableInstance{
		Name: name, ProjectID: "noctaxris-gcp-local", InstanceID: "creating",
		DisplayName: "Creating", State: "CREATING", Type: "PRODUCTION",
		LabelsJSON: "{}", ClustersJSON: "{}", CreatedAt: "2020-01-01T00:00:00Z",
	})
	if err != nil || !ok {
		t.Fatalf("create creating instance ok=%v err=%v", ok, err)
	}
	got, err := client.GetInstance(ctx, &adminpb.GetInstanceRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetState() != adminpb.Instance_CREATING {
		t.Fatalf("state=%v", got.GetState())
	}

	_, err = client.CreateInstance(ctx, &adminpb.CreateInstanceRequest{
		Parent: parent, InstanceId: "",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty id: %v", err)
	}
}
