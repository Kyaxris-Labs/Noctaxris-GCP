package cloudbuild_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/labtoken"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudbuild"
)

func envVal(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return strings.TrimPrefix(e, prefix), true
		}
	}
	return "", false
}

func TestNestedStepInjectsBuildSAAccessToken(t *testing.T) {
	const project = "noctaxris-gcp-local"
	const operatorRoot = "operator-root-sentinel"
	t.Setenv("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN", operatorRoot)
	cases := []struct {
		name      string
		body      string
		wantEmail string
		keepFOO   bool
	}{
		{
			name:      "explicit email",
			body:      `{"serviceAccount":"builder@` + project + `.iam.gserviceaccount.com","steps":[{"name":"alpine:3.23","env":["FOO=bar","CLOUDSDK_AUTH_ACCESS_TOKEN=stale"]}]}`,
			wantEmail: "builder@" + project + ".iam.gserviceaccount.com",
			keepFOO:   true,
		},
		{
			name:      "resource name",
			body:      `{"serviceAccount":"projects/` + project + `/serviceAccounts/ci-runner@` + project + `.iam.gserviceaccount.com","steps":[{"name":"alpine:3.23"}]}`,
			wantEmail: "ci-runner@" + project + ".iam.gserviceaccount.com",
		},
		{
			name:      "default compute SA",
			body:      `{"steps":[{"name":"alpine:3.23"}]}`,
			wantEmail: labtoken.DefaultComputeSAEmail(project),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, mux, svc := setupStepExec(t)
			var seen cloudbuild.BuildStep
			svc.StepRunner = &cloudbuild.EngineRunner{
				Store: st,
				ExecuteStep: func(_ context.Context, step cloudbuild.BuildStep) error {
					seen = step
					return nil
				},
			}
			created := postBuild(t, mux, tc.body)
			id, _ := created["id"].(string)
			got := getBuild(t, mux, id)
			if got["status"] != "SUCCESS" {
				t.Fatalf("status=%#v", got)
			}
			token, ok := envVal(seen.Env, "CLOUDSDK_AUTH_ACCESS_TOKEN")
			if !ok || token == "" {
				t.Fatalf("missing CLOUDSDK_AUTH_ACCESS_TOKEN: %#v", seen.Env)
			}
			if token == "stale" || token == operatorRoot {
				t.Fatal("stale step env token and operator root token must not be injected")
			}
			for _, e := range seen.Env {
				if strings.Contains(e, operatorRoot) {
					t.Fatalf("operator root token leaked into step env: %q", e)
				}
			}
			if !strings.HasPrefix(token, "ngsa_") {
				t.Fatalf("token=%q", token)
			}
			email, found, err := st.LookupAccessToken(authn.HashToken(token), time.Now().UTC())
			if err != nil || !found || email != tc.wantEmail {
				t.Fatalf("lookup email=%q found=%v err=%v want %q", email, found, err, tc.wantEmail)
			}
			if tc.keepFOO {
				if foo, ok := envVal(seen.Env, "FOO"); !ok || foo != "bar" {
					t.Fatalf("FOO=%q ok=%v", foo, ok)
				}
			}
			if _, ok := envVal(seen.Env, "CLOUDSDK_API_ENDPOINT_OVERRIDES_IAMCREDENTIALS"); ok {
				t.Fatal("endpoint overrides require host-gateway inject")
			}
		})
	}
}

func TestNestedStepHostGatewayEndpointOverrides(t *testing.T) {
	st, mux, svc := setupStepExec(t)
	t.Setenv(compute.EnvInjectHostGateway, "1")
	var seen cloudbuild.BuildStep
	svc.StepRunner = &cloudbuild.EngineRunner{
		Store: st,
		ExecuteStep: func(_ context.Context, step cloudbuild.BuildStep) error {
			seen = step
			return nil
		},
	}
	created := postBuild(t, mux, `{"serviceAccount":"builder@noctaxris-gcp-local.iam.gserviceaccount.com","steps":[{"name":"alpine:3.23"}]}`)
	id, _ := created["id"].(string)
	got := getBuild(t, mux, id)
	if got["status"] != "SUCCESS" {
		t.Fatalf("status=%#v", got)
	}
	base := "http://host.docker.internal:" + httpegress.LabListenPort + "/"
	if v, ok := envVal(seen.Env, "CLOUDSDK_API_ENDPOINT_OVERRIDES_IAMCREDENTIALS"); !ok || v != base {
		t.Fatalf("IAMCREDENTIALS override=%q ok=%v want %q env=%#v", v, ok, base, seen.Env)
	}
	if v, ok := envVal(seen.Env, "CLOUDSDK_API_ENDPOINT_OVERRIDES_IAM"); !ok || v != base {
		t.Fatalf("IAM override=%q ok=%v", v, ok)
	}
	if v, ok := envVal(seen.Env, "CLOUDSDK_API_ENDPOINT_OVERRIDES_STORAGE"); !ok || v != base {
		t.Fatalf("STORAGE override=%q ok=%v", v, ok)
	}
	if v, ok := envVal(seen.Env, "STORAGE_EMULATOR_HOST"); !ok || v != "host.docker.internal:"+httpegress.LabListenPort {
		t.Fatalf("STORAGE_EMULATOR_HOST=%q ok=%v", v, ok)
	}
	token, ok := envVal(seen.Env, "CLOUDSDK_AUTH_ACCESS_TOKEN")
	if !ok || !strings.HasPrefix(token, "ngsa_") {
		t.Fatalf("token=%q ok=%v", token, ok)
	}
	email, found, err := st.LookupAccessToken(authn.HashToken(token), time.Now().UTC())
	if err != nil || !found || email != "builder@noctaxris-gcp-local.iam.gserviceaccount.com" {
		t.Fatalf("lookup email=%q found=%v err=%v", email, found, err)
	}
}
