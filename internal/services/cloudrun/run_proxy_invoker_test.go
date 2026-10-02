package cloudrun_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudrun"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestNestedRunProxyRequiresInvoker(t *testing.T) {
	t.Setenv(cloudrun.EnvPublicURIBase, "")
	_, res := nestedBackend(t)
	eng := &fakeEngine{result: res}

	// reuse mountNestedCloudRun store pattern with anonymous /run/
	mux, st := mountNestedCloudRunAnon(t, eng)
	base := "/v2/projects/noctaxris-gcp-local/locations/us-central1/services"
	if rec := doJSON(mux, http.MethodPost, base+"?serviceId=web", nestedTemplateBody); rec.Code != http.StatusOK {
		t.Fatalf("create=%d %s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/run/noctaxris-gcp-local/us-central1/web/", nil)
	req.Header.Set("Authorization", "Bearer lab-secret")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unauthorized proxy want 403, got %d (auth must not be stripped through)", rec.Code)
	}

	name := "projects/noctaxris-gcp-local/locations/us-central1/services/web"
	if err := st.PutIAMPolicyJSON(name, authz.Policy{
		Bindings: []authz.Binding{{Role: "roles/run.invoker", Members: []string{"allUsers"}}},
	}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/run/noctaxris-gcp-local/us-central1/web/some/page", nil)
	req.Header.Set("Authorization", "Bearer lab-secret")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "hello-from-nested" {
		t.Fatalf("allUsers invoker: %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Seen-Auth") != "" {
		t.Fatal("Authorization must be stripped only after Invoker allow")
	}
}

func TestNestedRunProxyInvokerPrincipalNotAllUsers(t *testing.T) {
	t.Setenv(cloudrun.EnvPublicURIBase, "")
	_, res := nestedBackend(t)
	invoker := "invoker@example.com"
	mux, st := mountNestedCloudRunOptionalBearer(t, &fakeEngine{result: res}, invoker)
	base := "/v2/projects/noctaxris-gcp-local/locations/us-central1/services"
	if rec := doJSON(mux, http.MethodPost, base+"?serviceId=priv", nestedTemplateBody); rec.Code != http.StatusOK {
		t.Fatalf("create=%d %s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/run/noctaxris-gcp-local/us-central1/priv/", nil)
	req.Header.Set("Authorization", "Bearer invoker-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("invoker without binding want 403, got %d", rec.Code)
	}

	name := "projects/noctaxris-gcp-local/locations/us-central1/services/priv"
	if err := st.PutIAMPolicyJSON(name, authz.Policy{
		Bindings: []authz.Binding{{
			Role: "roles/run.invoker", Members: []string{"serviceAccount:" + invoker},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/run/noctaxris-gcp-local/us-central1/priv/ok", nil)
	req.Header.Set("Authorization", "Bearer invoker-token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "hello-from-nested" {
		t.Fatalf("bound invoker: %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Seen-Auth") != "" {
		t.Fatal("Authorization must be stripped after Invoker allow")
	}

	req = httptest.NewRequest(http.MethodGet, "/run/noctaxris-gcp-local/us-central1/priv/ok", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("anonymous without allUsers want 403, got %d", rec.Code)
	}
}

func mountNestedCloudRunAnon(t *testing.T, engine cloudrun.NestedRunner) (*http.ServeMux, *store.Store) {
	t.Helper()
	mux, st := mountNestedCloudRun(t, engine)
	// Remount with anonymous /run/ principal.
	mux = http.NewServeMux()
	const project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	svc := &cloudrun.Service{
		Store:   st,
		Authz:   &authz.Evaluator{Policies: st},
		Invoker: nil,
		Engine:  engine,
	}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		if strings.HasPrefix(r.URL.Path, "/run/") {
			return authn.Principal{}, false
		}
		return authn.Principal{Email: root, IsRoot: true}, true
	})
	return mux, st
}

// mountNestedCloudRunOptionalBearer mirrors production public-path optional Bearer:
// /run/ uses Authorization presence to supply a non-root principal email.
func mountNestedCloudRunOptionalBearer(t *testing.T, engine cloudrun.NestedRunner, edgeEmail string) (*http.ServeMux, *store.Store) {
	t.Helper()
	mux, st := mountNestedCloudRun(t, engine)
	mux = http.NewServeMux()
	const project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	svc := &cloudrun.Service{
		Store:   st,
		Authz:   &authz.Evaluator{Policies: st},
		Invoker: nil,
		Engine:  engine,
	}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		if strings.HasPrefix(r.URL.Path, "/run/") {
			if r.Header.Get("Authorization") == "" {
				return authn.Principal{}, false
			}
			return authn.Principal{Email: edgeEmail, IsRoot: false}, true
		}
		return authn.Principal{Email: root, IsRoot: true}, true
	})
	return mux, st
}
