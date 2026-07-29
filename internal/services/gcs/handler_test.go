package gcs_test

import (
	"encoding/json"
	"fmt"
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

func TestGCSNotificationConfigsCRUDAndPublish(t *testing.T) {
	mux, st, project := openGCS(t)
	host := "127.0.0.1:4588"
	topic := "projects/" + project + "/topics/gcs-notify"
	sub := "projects/" + project + "/subscriptions/gcs-notify-sub"
	if _, created, err := st.CreateTopic(topic, project); err != nil || !created {
		t.Fatalf("topic: %v %v", created, err)
	}
	if _, created, err := st.CreateSubscription(sub, topic, project, 10); err != nil || !created {
		t.Fatalf("sub: %v %v", created, err)
	}

	create := httptest.NewRequest(http.MethodPost, "/storage/v1/b?project="+project, strings.NewReader(`{"name":"notify-b","location":"US"}`))
	create.Header.Set("Content-Type", "application/json")
	create.Host = host
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, create)
	if rec.Code != http.StatusOK {
		t.Fatalf("create bucket: %d %s", rec.Code, rec.Body.String())
	}

	topicURI := "//pubsub.googleapis.com/" + topic
	insBody := fmt.Sprintf(`{"topic":%q,"payload_format":"JSON_API_V1","event_types":["OBJECT_FINALIZE","OBJECT_DELETE"],"object_name_prefix":"in/","custom_attributes":{"lab":"a5"}}`, topicURI)
	ins := httptest.NewRequest(http.MethodPost, "/storage/v1/b/notify-b/notificationConfigs", strings.NewReader(insBody))
	ins.Header.Set("Content-Type", "application/json")
	ins.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, ins)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert notification: %d %s", rec.Code, rec.Body.String())
	}
	var nResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &nResp); err != nil {
		t.Fatal(err)
	}
	if nResp["kind"] != "storage#notification" || nResp["id"] != "1" {
		t.Fatalf("notification = %#v", nResp)
	}
	if nResp["payload_format"] != "JSON_API_V1" {
		t.Fatalf("payload_format = %#v", nResp["payload_format"])
	}

	list := httptest.NewRequest(http.MethodGet, "/storage/v1/b/notify-b/notificationConfigs", nil)
	list.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, list)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Items) != 1 {
		t.Fatalf("items = %#v", listResp.Items)
	}

	get := httptest.NewRequest(http.MethodGet, "/storage/v1/b/notify-b/notificationConfigs/1", nil)
	get.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, get)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}

	up := httptest.NewRequest(http.MethodPost, "/upload/storage/v1/b/notify-b/o?uploadType=media&name=in/hello.txt", strings.NewReader("hello-notify"))
	up.Header.Set("Content-Type", "text/plain")
	up.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, up)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	var msgs []store.PubSubMessage
	for time.Now().Before(deadline) {
		var err error
		msgs, err = st.Pull(sub, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(msgs) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected finalize message, got %#v", msgs)
	}
	var attrs map[string]string
	if err := json.Unmarshal([]byte(msgs[0].AttributesJSON), &attrs); err != nil {
		t.Fatal(err)
	}
	if attrs["eventType"] != "OBJECT_FINALIZE" || attrs["objectId"] != "in/hello.txt" || attrs["lab"] != "a5" {
		t.Fatalf("attrs = %#v", attrs)
	}
	if !strings.Contains(string(msgs[0].Data), `"name":"in/hello.txt"`) {
		t.Fatalf("payload = %s", msgs[0].Data)
	}
	if err := st.Acknowledge(sub, []string{msgs[0].AckID}); err != nil {
		t.Fatal(err)
	}

	// Prefix miss should not publish.
	skip := httptest.NewRequest(http.MethodPost, "/upload/storage/v1/b/notify-b/o?uploadType=media&name=out/skip.txt", strings.NewReader("skip"))
	skip.Header.Set("Content-Type", "text/plain")
	skip.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, skip)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload skip: %d %s", rec.Code, rec.Body.String())
	}
	time.Sleep(50 * time.Millisecond)
	msgs, err := st.Pull(sub, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("prefix miss published: %#v", msgs)
	}

	delObj := httptest.NewRequest(http.MethodDelete, "/storage/v1/b/notify-b/o/in/hello.txt", nil)
	delObj.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, delObj)
	if rec.Code != http.StatusNoContent && rec.Code != http.StatusOK {
		t.Fatalf("delete object: %d %s", rec.Code, rec.Body.String())
	}
	deadline = time.Now().Add(2 * time.Second)
	msgs = nil
	for time.Now().Before(deadline) {
		var err error
		msgs, err = st.Pull(sub, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(msgs) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected delete message, got %#v", msgs)
	}
	if err := json.Unmarshal([]byte(msgs[0].AttributesJSON), &attrs); err != nil {
		t.Fatal(err)
	}
	if attrs["eventType"] != "OBJECT_DELETE" {
		t.Fatalf("attrs = %#v", attrs)
	}
	if err := st.Acknowledge(sub, []string{msgs[0].AckID}); err != nil {
		t.Fatal(err)
	}

	del := httptest.NewRequest(http.MethodDelete, "/storage/v1/b/notify-b/notificationConfigs/1", nil)
	del.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, del)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete notification: %d %s", rec.Code, rec.Body.String())
	}
	get = httptest.NewRequest(http.MethodGet, "/storage/v1/b/notify-b/notificationConfigs/1", nil)
	get.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, get)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGCSNotificationConfigsAuthzDeny(t *testing.T) {
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
	if _, created, err := st.CreateBucket("deny-notify", project, "US", "STANDARD"); err != nil || !created {
		t.Fatalf("bucket: %v %v", created, err)
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
	body := `{"topic":"//pubsub.googleapis.com/projects/noctaxris-gcp-local/topics/t","payload_format":"JSON_API_V1"}`
	req := httptest.NewRequest(http.MethodPost, "/storage/v1/b/deny-notify/notificationConfigs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "127.0.0.1:4588"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

