package gcs_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	gcshandler "github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/gcs"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func openGCS(t *testing.T) (*http.ServeMux, *store.Store, string) {
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
	t.Cleanup(func() { _ = st.Close() })
	const project = "noctaxris-gcp-local"
	const rootEmail = "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, rootEmail); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h := &gcshandler.Handler{
		Store:          st,
		Authz:          &authz.Evaluator{Policies: st},
		DefaultProject: project,
		Principal: func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: rootEmail, IsRoot: true}, true
		},
	}
	h.Register(mux)
	return mux, st, project
}

func TestGCSRetentionLockedOverwriteAndClearDeny(t *testing.T) {
	mux, _, project := openGCS(t)
	host := "127.0.0.1:4588"

	create := httptest.NewRequest(http.MethodPost, "/storage/v1/b?project="+project, strings.NewReader(`{"name":"retain-h","location":"US"}`))
	create.Header.Set("Content-Type", "application/json")
	create.Host = host
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, create)
	if rec.Code != http.StatusOK {
		t.Fatalf("create bucket: %d %s", rec.Code, rec.Body.String())
	}

	patch := httptest.NewRequest(http.MethodPatch, "/storage/v1/b/retain-h",
		strings.NewReader(`{"retentionPolicy":{"retentionPeriod":"3600"}}`))
	patch.Header.Set("Content-Type", "application/json")
	patch.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, patch)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch retention: %d %s", rec.Code, rec.Body.String())
	}

	up := httptest.NewRequest(http.MethodPost, "/upload/storage/v1/b/retain-h/o?uploadType=media&name=held.txt", strings.NewReader("held"))
	up.Header.Set("Content-Type", "text/plain")
	up.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, up)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}

	over := httptest.NewRequest(http.MethodPut, "/upload/storage/v1/b/retain-h/o?uploadType=media&name=held.txt", strings.NewReader("new"))
	over.Header.Set("Content-Type", "text/plain")
	over.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, over)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("overwrite while retained: %d %s", rec.Code, rec.Body.String())
	}

	lock := httptest.NewRequest(http.MethodPatch, "/storage/v1/b/retain-h",
		strings.NewReader(`{"retentionPolicy":{"retentionPeriod":"3600","isLocked":true}}`))
	lock.Header.Set("Content-Type", "application/json")
	lock.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, lock)
	if rec.Code != http.StatusOK {
		t.Fatalf("lock retention: %d %s", rec.Code, rec.Body.String())
	}

	shorten := httptest.NewRequest(http.MethodPatch, "/storage/v1/b/retain-h",
		strings.NewReader(`{"retentionPolicy":{"retentionPeriod":"60"}}`))
	shorten.Header.Set("Content-Type", "application/json")
	shorten.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, shorten)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("shorten locked: %d %s", rec.Code, rec.Body.String())
	}

	clear := httptest.NewRequest(http.MethodPatch, "/storage/v1/b/retain-h",
		strings.NewReader(`{"retentionPolicy":{"retentionPeriod":"0"}}`))
	clear.Header.Set("Content-Type", "application/json")
	clear.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, clear)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("clear locked retention: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGCSV4SignedURLHappyAndDeny(t *testing.T) {
	mux, st, project := openGCS(t)
	host := "127.0.0.1:4588"

	req := httptest.NewRequest(http.MethodPost, "/storage/v1/b?project="+project, strings.NewReader(`{"name":"signed-h"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Host = host
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create bucket: %d %s", rec.Code, rec.Body.String())
	}

	up := httptest.NewRequest(http.MethodPost, "/upload/storage/v1/b/signed-h/o?uploadType=media&name=hello.txt", strings.NewReader("payload"))
	up.Header.Set("Content-Type", "text/plain")
	up.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, up)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}

	unauthMux := http.NewServeMux()
	ua := &gcshandler.Handler{
		Store:          st,
		Authz:          &authz.Evaluator{Policies: st},
		DefaultProject: project,
		Principal: func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{}, false
		},
	}
	ua.Register(unauthMux)
	noAuth := httptest.NewRequest(http.MethodGet, "/storage/v1/b/signed-h/o/hello.txt?alt=media", nil)
	noAuth.Host = host
	rec = httptest.NewRecorder()
	unauthMux.ServeHTTP(rec, noAuth)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned GET without auth: %d %s", rec.Code, rec.Body.String())
	}

	gen := httptest.NewRequest(http.MethodPost, "/storage/v1/b/signed-h/o/hello.txt:generateSignedUrl",
		strings.NewReader(`{"method":"GET","expires":600,"alt":"media"}`))
	gen.Header.Set("Content-Type", "application/json")
	gen.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, gen)
	if rec.Code != http.StatusOK {
		t.Fatalf("generateSignedUrl: %d %s", rec.Code, rec.Body.String())
	}
	var genResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &genResp); err != nil {
		t.Fatal(err)
	}
	signedURL, _ := genResp["signedUrl"].(string)
	u, err := url.Parse(signedURL)
	if err != nil {
		t.Fatal(err)
	}

	dl := httptest.NewRequest(http.MethodGet, u.RequestURI(), nil)
	dl.Host = host
	rec = httptest.NewRecorder()
	unauthMux.ServeHTTP(rec, dl)
	if rec.Code != http.StatusOK {
		t.Fatalf("signed GET: %d %s", rec.Code, rec.Body.String())
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "payload" {
		t.Fatalf("body = %q", body)
	}

	badSig := *u
	q := badSig.Query()
	sig := q.Get("X-Goog-Signature")
	if len(sig) < 2 {
		t.Fatal("missing signature")
	}
	q.Set("X-Goog-Signature", sig[:len(sig)-2]+"ff")
	badSig.RawQuery = q.Encode()
	bad := httptest.NewRequest(http.MethodGet, badSig.RequestURI(), nil)
	bad.Host = host
	rec = httptest.NewRecorder()
	unauthMux.ServeHTTP(rec, bad)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tampered signature: %d %s", rec.Code, rec.Body.String())
	}

	wrongMethod := httptest.NewRequest(http.MethodDelete, u.RequestURI(), nil)
	wrongMethod.Host = host
	rec = httptest.NewRecorder()
	unauthMux.ServeHTTP(rec, wrongMethod)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong HTTP method: %d %s", rec.Code, rec.Body.String())
	}

	fixedNow := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	expired, err := store.GenerateV4SignedURL(store.SignedURLRequest{
		Method:  "GET",
		Host:    host,
		Path:    "/storage/v1/b/signed-h/o/hello.txt",
		Expires: 60,
		Query:   url.Values{"alt": []string{"media"}},
		Now:     fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	expU, err := url.Parse(expired)
	if err != nil {
		t.Fatal(err)
	}
	expReq := httptest.NewRequest(http.MethodGet, expU.RequestURI(), nil)
	expReq.Host = host
	rec = httptest.NewRecorder()
	unauthMux.ServeHTTP(rec, expReq)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired signed URL: %d %s", rec.Code, rec.Body.String())
	}

	putGen := httptest.NewRequest(http.MethodPost, "/storage/v1/b/signed-h/o/put-obj.txt:generateSignedUrl",
		strings.NewReader(`{"method":"PUT","expires":600}`))
	putGen.Header.Set("Content-Type", "application/json")
	putGen.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, putGen)
	if rec.Code != http.StatusOK {
		t.Fatalf("generate PUT url: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &genResp)
	putURL, _ := url.Parse(genResp["signedUrl"].(string))
	put := httptest.NewRequest(http.MethodPut, putURL.RequestURI(), strings.NewReader("from-put"))
	put.Header.Set("Content-Type", "text/plain")
	put.Host = host
	rec = httptest.NewRecorder()
	unauthMux.ServeHTTP(rec, put)
	if rec.Code != http.StatusOK {
		t.Fatalf("signed PUT: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGCSAuthzDenyNonRoot(t *testing.T) {
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
	const project = "noctaxris-gcp-local"
	const rootEmail = "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, rootEmail); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h := &gcshandler.Handler{
		Store:          st,
		Authz:          &authz.Evaluator{Policies: st},
		DefaultProject: project,
		Principal: func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
		},
	}
	h.Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/storage/v1/b?project="+project, strings.NewReader(`{"name":"deny-b","location":"US"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "127.0.0.1:4588"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
