package pubsub_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/pubsub"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type vpcscPubSubFixture struct {
	mux     *http.ServeMux
	svc     *pubsub.Service
	st      *store.Store
	who     authn.Principal
	project string
	topic   string
}

func setupVPCSCPubSub(t *testing.T) *vpcscPubSubFixture {
	t.Helper()
	t.Setenv("NOCTAXRIS_GCP_VPCSC_ENFORCE", "1")
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
	rootSA := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, rootSA); err != nil {
		t.Fatal(err)
	}
	if err := st.MigrateAccessContextManager(); err != nil {
		t.Fatal(err)
	}
	statusJSON, _ := json.Marshal(map[string]any{
		"resources":          []string{"projects/" + project},
		"restrictedServices": []string{"pubsub.googleapis.com"},
	})
	polName := store.AccessPolicyResourceName("ps-pol")
	if ok, err := st.CreateAccessPolicy(store.AccessPolicy{
		Name: polName, PolicyID: "ps-pol", Parent: "organizations/noctaxris-gcp-org", Title: "PubSub",
	}); err != nil || !ok {
		t.Fatalf("policy ok=%v err=%v", ok, err)
	}
	if ok, err := st.CreateServicePerimeter(store.ServicePerimeter{
		Name: store.ServicePerimeterResourceName("ps-pol", "ps-edge"), PolicyName: polName,
		PerimeterID: "ps-edge", Title: "pubsub", StatusJSON: string(statusJSON),
	}); err != nil || !ok {
		t.Fatalf("perimeter ok=%v err=%v", ok, err)
	}
	topic := "projects/" + project + "/topics/vpc-topic"
	if _, ok, err := st.CreateTopic(topic, project); err != nil || !ok {
		t.Fatalf("topic ok=%v err=%v", ok, err)
	}

	f := &vpcscPubSubFixture{
		st:      st,
		who:     authn.Principal{Email: rootSA, IsRoot: true},
		project: project,
		topic:   topic,
	}
	svc := &pubsub.Service{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		Principal: func(context.Context) (authn.Principal, error) {
			return f.who, nil
		},
	}
	mux := http.NewServeMux()
	svc.RegisterREST(mux, func(*http.Request) (authn.Principal, bool) { return f.who, true })
	f.mux = mux
	f.svc = svc
	return f
}

func (f *vpcscPubSubFixture) grantEditor(t *testing.T, email string) {
	t.Helper()
	member := email
	if !strings.Contains(email, ":") {
		member = "serviceAccount:" + email
	}
	if err := f.st.PutIAMPolicyJSON("projects/"+f.project, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/editor",
			Members: []string{member},
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

func (f *vpcscPubSubFixture) restPublish() *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost,
		"/v1/projects/"+f.project+"/topics/vpc-topic:publish",
		strings.NewReader(`{"messages":[{"data":"aGk="}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

func (f *vpcscPubSubFixture) grpcPublish() error {
	_, err := f.svc.Publish(context.Background(), &pubsubpb.PublishRequest{
		Topic:    f.topic,
		Messages: []*pubsubpb.PubsubMessage{{Data: []byte("hi")}},
	})
	return err
}

func TestVPCSCPubSubPublishWIFUnresolvedDenies(t *testing.T) {
	f := setupVPCSCPubSub(t)
	email := "wif:missing:alice"
	f.grantEditor(t, email)
	f.who = authn.Principal{Email: email, IsRoot: false}
	rec := f.restPublish()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unresolved WIF REST publish status=%d body=%s", rec.Code, rec.Body.String())
	}
	err := f.grpcPublish()
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("unresolved WIF gRPC publish err=%v", err)
	}
}

func TestVPCSCPubSubPublishWIFOtherProjectDenies(t *testing.T) {
	f := setupVPCSCPubSub(t)
	pool, err := f.st.CreateWIFPool("other-proj", "global", "ps-pool", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CreateWIFProvider(pool.Name, "oidc-lab", "", "", "", "{}", "[]", false); err != nil {
		t.Fatal(err)
	}
	email := "wif:oidc-lab:alice"
	f.grantEditor(t, email)
	f.who = authn.Principal{Email: email, IsRoot: false}
	rec := f.restPublish()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-project WIF REST publish status=%d body=%s", rec.Code, rec.Body.String())
	}
	err = f.grpcPublish()
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cross-project WIF gRPC publish err=%v", err)
	}
}

func TestVPCSCPubSubPublishWIFSameProjectAllows(t *testing.T) {
	f := setupVPCSCPubSub(t)
	pool, err := f.st.CreateWIFPool(f.project, "global", "ps-pool", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CreateWIFProvider(pool.Name, "oidc-lab", "", "", "", "{}", "[]", false); err != nil {
		t.Fatal(err)
	}
	email := "wif:oidc-lab:alice"
	f.grantEditor(t, email)
	f.who = authn.Principal{Email: email, IsRoot: false}
	rec := f.restPublish()
	if rec.Code != http.StatusOK {
		t.Fatalf("same-project WIF REST publish status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := f.grpcPublish(); err != nil {
		t.Fatalf("same-project WIF gRPC publish err=%v", err)
	}
}

func TestVPCSCPubSubPublishRootSkipsCallerCheck(t *testing.T) {
	f := setupVPCSCPubSub(t)
	pool, err := f.st.CreateWIFPool("other-proj", "global", "ps-pool", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CreateWIFProvider(pool.Name, "oidc-lab", "", "", "", "{}", "[]", false); err != nil {
		t.Fatal(err)
	}
	f.who = authn.Principal{Email: "wif:oidc-lab:alice", IsRoot: true}
	rec := f.restPublish()
	if rec.Code != http.StatusOK {
		t.Fatalf("root REST publish status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := f.grpcPublish(); err != nil {
		t.Fatalf("root gRPC publish err=%v", err)
	}
}
