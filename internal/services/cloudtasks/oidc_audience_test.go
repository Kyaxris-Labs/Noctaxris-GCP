package cloudtasks_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/jwtutil"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudtasks"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestTasksOIDCHonorsAudience(t *testing.T) {
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

	const project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	saEmail := "tasks-oidc@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureServiceAccount(project, saEmail, "tasks oidc"); err != nil {
		t.Fatal(err)
	}

	var (
		mu      sync.Mutex
		gotAuth string
	)
	capture := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotAuth = r.Header.Get("Authorization")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	auth := &authn.Authenticator{
		RootServiceAccount: root,
		RootAccessToken:    "test-root-token",
		Tokens:             st,
	}
	mux := http.NewServeMux()
	svc := &cloudtasks.Service{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		HTTPClient: &http.Client{
			Transport: &captureRoundTrip{h: capture},
		},
	}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		p, err := auth.AuthenticateRequest(r)
		return p, err == nil
	})

	qBase := "/v2/projects/" + project + "/locations/" + cloudtasks.DefaultLocation + "/queues"
	req := httptest.NewRequest(http.MethodPost, qBase+"?queueId=oidc-q", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer test-root-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create queue status=%d body=%s", rec.Code, rec.Body.String())
	}

	targetURL := "http://127.0.0.1:4588/v1/lab-oidc-sink"
	wantAud := "https://tasks-custom-aud.example"
	taskBody := `{"taskId":"t-aud","task":{"httpRequest":{"url":"` + targetURL + `","httpMethod":"POST","oidcToken":{"serviceAccountEmail":"` + saEmail + `","audience":"` + wantAud + `"},"body":"e30="}}}`
	req = httptest.NewRequest(http.MethodPost, qBase+"/oidc-q/tasks", bytes.NewReader([]byte(taskBody)))
	req.Header.Set("Authorization", "Bearer test-root-token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create task status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, qBase+"/oidc-q/tasks/t-aud:run", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer test-root-token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("task :run status=%d body=%s", rec.Code, rec.Body.String())
	}

	mu.Lock()
	authzHeader := gotAuth
	mu.Unlock()
	if !strings.HasPrefix(authzHeader, "Bearer ") {
		t.Fatalf("missing Bearer Authorization: %q", authzHeader)
	}
	token := strings.TrimPrefix(authzHeader, "Bearer ")
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[2] == "" {
		t.Fatalf("expected RS256 JWT, got %q", token)
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var header map[string]any
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		t.Fatal(err)
	}
	if header["alg"] != "RS256" {
		t.Fatalf("alg=%v want RS256", header["alg"])
	}
	jwks, err := jwtutil.MarshalLabOIDCJWKS()
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwtutil.VerifyCompactRS256(token, jwks)
	if err != nil {
		t.Fatal(err)
	}
	if claims["aud"] != wantAud || claims["email"] != saEmail {
		t.Fatalf("claims=%#v want aud=%q email=%q", claims, wantAud, saEmail)
	}
}
