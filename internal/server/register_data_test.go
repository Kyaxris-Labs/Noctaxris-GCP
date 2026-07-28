package server_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
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
)

func TestGCSCreateUploadDownload(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID

	createBody := `{"name":"lab-gcs-1","location":"US"}`
	req := httptest.NewRequest(http.MethodPost, "/storage/v1/b?project="+project, strings.NewReader(createBody))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create bucket status=%d body=%s", rec.Code, rec.Body.String())
	}

	payload := []byte("roundtrip-bytes")
	up := httptest.NewRequest(http.MethodPost, "/upload/storage/v1/b/lab-gcs-1/o?uploadType=media&name=dir/file.txt", bytes.NewReader(payload))
	up.Header.Set("Authorization", auth)
	up.Header.Set("Content-Type", "text/plain")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, up)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", rec.Code, rec.Body.String())
	}

	dl := httptest.NewRequest(http.MethodGet, "/storage/v1/b/lab-gcs-1/o/dir/file.txt?alt=media", nil)
	dl.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, dl)
	if rec.Code != http.StatusOK {
		t.Fatalf("download status=%d body=%s", rec.Code, rec.Body.String())
	}
	body, _ := io.ReadAll(rec.Body)
	if !bytes.Equal(body, payload) {
		t.Fatalf("download = %q", body)
	}
}

func TestGCSComposeCopyPatchAndIAM(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID

	req := httptest.NewRequest(http.MethodPost, "/storage/v1/b?project="+project, strings.NewReader(`{"name":"lab-gcs-2"}`))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create bucket: %d %s", rec.Code, rec.Body.String())
	}
	for _, name := range []string{"p1", "p2"} {
		up := httptest.NewRequest(http.MethodPost, "/upload/storage/v1/b/lab-gcs-2/o?uploadType=media&name="+name, strings.NewReader(name))
		up.Header.Set("Authorization", auth)
		up.Header.Set("Content-Type", "text/plain")
		rec = httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, up)
		if rec.Code != http.StatusOK {
			t.Fatalf("upload %s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	composeBody := `{"sourceObjects":[{"name":"p1"},{"name":"p2"}],"destination":{"contentType":"text/plain"}}`
	comp := httptest.NewRequest(http.MethodPost, "/storage/v1/b/lab-gcs-2/o/out/compose", strings.NewReader(composeBody))
	comp.Header.Set("Authorization", auth)
	comp.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, comp)
	if rec.Code != http.StatusOK {
		t.Fatalf("compose: %d %s", rec.Code, rec.Body.String())
	}
	copyReq := httptest.NewRequest(http.MethodPost, "/storage/v1/b/lab-gcs-2/o/out/copyTo/b/lab-gcs-2/o/out-copy", nil)
	copyReq.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, copyReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("copy: %d %s", rec.Code, rec.Body.String())
	}
	patchObj := httptest.NewRequest(http.MethodPatch, "/storage/v1/b/lab-gcs-2/o/out-copy", strings.NewReader(`{"metadata":{"k":"v"}}`))
	patchObj.Header.Set("Authorization", auth)
	patchObj.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, patchObj)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch object: %d %s", rec.Code, rec.Body.String())
	}
	patchB := httptest.NewRequest(http.MethodPatch, "/storage/v1/b/lab-gcs-2", strings.NewReader(`{"labels":{"env":"lab"}}`))
	patchB.Header.Set("Authorization", auth)
	patchB.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, patchB)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch bucket: %d %s", rec.Code, rec.Body.String())
	}
	setIAM := httptest.NewRequest(http.MethodPut, "/storage/v1/b/lab-gcs-2/iam", strings.NewReader(`{"etag":"ACAB","bindings":[{"role":"roles/storage.admin","members":["allAuthenticatedUsers"]}]}`))
	setIAM.Header.Set("Authorization", auth)
	setIAM.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, setIAM)
	if rec.Code != http.StatusOK {
		t.Fatalf("set iam: %d %s", rec.Code, rec.Body.String())
	}
	getIAM := httptest.NewRequest(http.MethodGet, "/storage/v1/b/lab-gcs-2/iam", nil)
	getIAM.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, getIAM)
	if rec.Code != http.StatusOK {
		t.Fatalf("get iam: %d %s", rec.Code, rec.Body.String())
	}
	testIAM := httptest.NewRequest(http.MethodGet, "/storage/v1/b/lab-gcs-2/iam/testPermissions?permissions=storage.objects.get", nil)
	testIAM.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, testIAM)
	if rec.Code != http.StatusOK {
		t.Fatalf("test iam: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPubSubRESTPublishPull(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID

	putTopic := httptest.NewRequest(http.MethodPut, "/v1/projects/"+project+"/topics/rest-topic", strings.NewReader(`{}`))
	putTopic.Header.Set("Authorization", auth)
	putTopic.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, putTopic)
	if rec.Code != http.StatusOK {
		t.Fatalf("create topic: %d %s", rec.Code, rec.Body.String())
	}
	subBody := `{"topic":"projects/` + project + `/topics/rest-topic","ackDeadlineSeconds":10}`
	putSub := httptest.NewRequest(http.MethodPut, "/v1/projects/"+project+"/subscriptions/rest-sub", strings.NewReader(subBody))
	putSub.Header.Set("Authorization", auth)
	putSub.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, putSub)
	if rec.Code != http.StatusOK {
		t.Fatalf("create sub: %d %s", rec.Code, rec.Body.String())
	}
	pubBody := `{"messages":[{"data":"` + base64.StdEncoding.EncodeToString([]byte("hello-rest")) + `"}]}`
	pub := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/topics/rest-topic:publish", strings.NewReader(pubBody))
	pub.Header.Set("Authorization", auth)
	pub.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, pub)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", rec.Code, rec.Body.String())
	}
	pull := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/subscriptions/rest-sub:pull", strings.NewReader(`{"maxMessages":10}`))
	pull.Header.Set("Authorization", auth)
	pull.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, pull)
	if rec.Code != http.StatusOK {
		t.Fatalf("pull: %d %s", rec.Code, rec.Body.String())
	}
	var pullResp struct {
		ReceivedMessages []struct {
			AckID string `json:"ackId"`
		} `json:"receivedMessages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pullResp); err != nil {
		t.Fatal(err)
	}
	if len(pullResp.ReceivedMessages) != 1 {
		t.Fatalf("pull = %s", rec.Body.String())
	}
	ackBody, _ := json.Marshal(map[string]any{"ackIds": []string{pullResp.ReceivedMessages[0].AckID}})
	ack := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/subscriptions/rest-sub:acknowledge", bytes.NewReader(ackBody))
	ack.Header.Set("Authorization", auth)
	ack.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, ack)
	if rec.Code != http.StatusOK {
		t.Fatalf("ack: %d %s", rec.Code, rec.Body.String())
	}
	mod := httptest.NewRequest(http.MethodPatch, "/v1/projects/"+project+"/subscriptions/rest-sub", strings.NewReader(`{"ackDeadlineSeconds":20}`))
	mod.Header.Set("Authorization", auth)
	mod.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, mod)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch sub: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSecretPatchAndIAM(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID

	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/secrets?secretId=iam-secret", strings.NewReader(`{}`))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	patch := httptest.NewRequest(http.MethodPatch, "/v1/projects/"+project+"/secrets/iam-secret", strings.NewReader(`{"labels":{"env":"lab"}}`))
	patch.Header.Set("Authorization", auth)
	patch.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, patch)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	setIAM := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/secrets/iam-secret:setIamPolicy", strings.NewReader(`{"policy":{"etag":"ACAB","bindings":[{"role":"roles/secretmanager.admin","members":["allAuthenticatedUsers"]}]}}`))
	setIAM.Header.Set("Authorization", auth)
	setIAM.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, setIAM)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIamPolicy: %d %s", rec.Code, rec.Body.String())
	}
	getIAM := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/secrets/iam-secret:getIamPolicy", strings.NewReader(`{}`))
	getIAM.Header.Set("Authorization", auth)
	getIAM.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, getIAM)
	if rec.Code != http.StatusOK {
		t.Fatalf("getIamPolicy: %d %s", rec.Code, rec.Body.String())
	}
	testIAM := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/secrets/iam-secret:testIamPermissions", strings.NewReader(`{"permissions":["secretmanager.secrets.get"]}`))
	testIAM.Header.Set("Authorization", auth)
	testIAM.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, testIAM)
	if rec.Code != http.StatusOK {
		t.Fatalf("testIamPermissions: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSecretAddVersionAccess(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID

	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/secrets?secretId=db-pass", strings.NewReader(`{}`))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create secret status=%d body=%s", rec.Code, rec.Body.String())
	}

	addBody := map[string]any{
		"payload": map[string]string{
			"data": base64.StdEncoding.EncodeToString([]byte("hunter2")),
		},
	}
	raw, _ := json.Marshal(addBody)
	add := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/secrets/db-pass:addVersion", bytes.NewReader(raw))
	add.Header.Set("Authorization", auth)
	add.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, add)
	if rec.Code != http.StatusOK {
		t.Fatalf("addVersion status=%d body=%s", rec.Code, rec.Body.String())
	}

	access := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/secrets/db-pass/versions/latest:access", nil)
	access.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, access)
	if rec.Code != http.StatusOK {
		t.Fatalf("access status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Payload struct {
			Data string `json:"data"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	got, err := base64.StdEncoding.DecodeString(resp.Payload.Data)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hunter2" {
		t.Fatalf("payload = %q", got)
	}
}

func TestPubSubPublishPull(t *testing.T) {
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
	rootSA := "root@noctaxris-gcp-local.iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, rootSA); err != nil {
		t.Fatal(err)
	}

	ps := &pubsub.Service{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		Principal: func(ctx context.Context) (authn.Principal, error) {
			return authn.Principal{Email: rootSA, IsRoot: true}, nil
		},
	}
	ctx := context.Background()
	topicName := "projects/" + project + "/topics/lab-topic"
	subName := "projects/" + project + "/subscriptions/lab-sub"

	if _, err := ps.CreateTopic(ctx, &pubsubpb.Topic{Name: topicName}); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.CreateSubscription(ctx, &pubsubpb.Subscription{
		Name:               subName,
		Topic:              topicName,
		AckDeadlineSeconds: 10,
	}); err != nil {
		t.Fatal(err)
	}
	pub, err := ps.Publish(ctx, &pubsubpb.PublishRequest{
		Topic: topicName,
		Messages: []*pubsubpb.PubsubMessage{
			{Data: []byte("hello-pubsub"), Attributes: map[string]string{"a": "1"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pub.MessageIds) != 1 {
		t.Fatalf("message ids = %#v", pub.MessageIds)
	}
	pull, err := ps.Pull(ctx, &pubsubpb.PullRequest{Subscription: subName, MaxMessages: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(pull.ReceivedMessages) != 1 || string(pull.ReceivedMessages[0].Message.Data) != "hello-pubsub" {
		t.Fatalf("pull = %#v", pull.ReceivedMessages)
	}
	if _, err := ps.Acknowledge(ctx, &pubsubpb.AcknowledgeRequest{
		Subscription: subName,
		AckIds:       []string{pull.ReceivedMessages[0].AckId},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGCSRewriteResumableAndPreconditions(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID

	req := httptest.NewRequest(http.MethodPost, "/storage/v1/b?project="+project, strings.NewReader(`{"name":"lab-gcs-deep"}`))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	up := httptest.NewRequest(http.MethodPost, "/upload/storage/v1/b/lab-gcs-deep/o?uploadType=media&name=dir/a.txt", strings.NewReader("hello"))
	up.Header.Set("Authorization", auth)
	up.Header.Set("Content-Type", "text/plain")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, up)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var uploaded struct {
		Generation string `json:"generation"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &uploaded)

	init := httptest.NewRequest(http.MethodPost, "/upload/storage/v1/b/lab-gcs-deep/o?uploadType=resumable&name=dir/b.txt", strings.NewReader(`{"contentType":"text/plain"}`))
	init.Header.Set("Authorization", auth)
	init.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, init)
	if rec.Code != http.StatusOK {
		t.Fatalf("resumable init: %d %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatal("missing Location")
	}
	put := httptest.NewRequest(http.MethodPut, loc, strings.NewReader("resumable-body"))
	put.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, put)
	if rec.Code != http.StatusOK {
		t.Fatalf("resumable put: %d %s", rec.Code, rec.Body.String())
	}

	rw := httptest.NewRequest(http.MethodPost, "/storage/v1/b/lab-gcs-deep/o/dir/a.txt/rewriteTo/b/lab-gcs-deep/o/dir/c.txt", nil)
	rw.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, rw)
	if rec.Code != http.StatusOK {
		t.Fatalf("rewrite: %d %s", rec.Code, rec.Body.String())
	}
	var rewriteResp struct {
		Done bool `json:"done"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &rewriteResp)
	if !rewriteResp.Done {
		t.Fatal("rewrite not done")
	}

	list := httptest.NewRequest(http.MethodGet, "/storage/v1/b/lab-gcs-deep/o?delimiter=/", nil)
	list.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, list)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		Prefixes []string `json:"prefixes"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &listResp)
	if len(listResp.Prefixes) == 0 {
		t.Fatalf("expected prefixes, got %s", rec.Body.String())
	}

	bad := httptest.NewRequest(http.MethodGet, "/storage/v1/b/lab-gcs-deep/o/dir/a.txt?ifGenerationMatch=999999", nil)
	bad.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, bad)
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected precondition failed, got %d %s", rec.Code, rec.Body.String())
	}
	okGet := httptest.NewRequest(http.MethodGet, "/storage/v1/b/lab-gcs-deep/o/dir/a.txt?ifGenerationMatch="+uploaded.Generation, nil)
	okGet.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, okGet)
	if rec.Code != http.StatusOK {
		t.Fatalf("get with match: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPubSubFilterSeekPushConfig(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID

	putTopic := httptest.NewRequest(http.MethodPut, "/v1/projects/"+project+"/topics/filter-topic", strings.NewReader(`{}`))
	putTopic.Header.Set("Authorization", auth)
	putTopic.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, putTopic)
	if rec.Code != http.StatusOK {
		t.Fatalf("topic: %d %s", rec.Code, rec.Body.String())
	}
	subBody := `{"topic":"projects/` + project + `/topics/filter-topic","ackDeadlineSeconds":10,"filter":"attributes.region = \"us\""}`
	putSub := httptest.NewRequest(http.MethodPut, "/v1/projects/"+project+"/subscriptions/filter-sub", strings.NewReader(subBody))
	putSub.Header.Set("Authorization", auth)
	putSub.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, putSub)
	if rec.Code != http.StatusOK {
		t.Fatalf("sub: %d %s", rec.Code, rec.Body.String())
	}
	push := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/subscriptions/filter-sub:modifyPushConfig", strings.NewReader(`{"pushConfig":{"pushEndpoint":"http://127.0.0.1:9/push"}}`))
	push.Header.Set("Authorization", auth)
	push.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, push)
	if rec.Code != http.StatusOK {
		t.Fatalf("modifyPushConfig: %d %s", rec.Code, rec.Body.String())
	}
	clearPush := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/subscriptions/filter-sub:modifyPushConfig", strings.NewReader(`{"pushConfig":{"pushEndpoint":""}}`))
	clearPush.Header.Set("Authorization", auth)
	clearPush.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, clearPush)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear push: %d %s", rec.Code, rec.Body.String())
	}
	pubBody := `{"messages":[{"data":"` + base64.StdEncoding.EncodeToString([]byte("us-msg")) + `","attributes":{"region":"us"}},{"data":"` + base64.StdEncoding.EncodeToString([]byte("eu-msg")) + `","attributes":{"region":"eu"}}]}`
	pub := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/topics/filter-topic:publish", strings.NewReader(pubBody))
	pub.Header.Set("Authorization", auth)
	pub.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, pub)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", rec.Code, rec.Body.String())
	}
	pull := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/subscriptions/filter-sub:pull", strings.NewReader(`{"maxMessages":10}`))
	pull.Header.Set("Authorization", auth)
	pull.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, pull)
	if rec.Code != http.StatusOK {
		t.Fatalf("pull: %d %s", rec.Code, rec.Body.String())
	}
	var pullResp struct {
		ReceivedMessages []struct {
			Message struct {
				Data string `json:"data"`
			} `json:"message"`
		} `json:"receivedMessages"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &pullResp)
	if len(pullResp.ReceivedMessages) != 1 {
		t.Fatalf("expected 1 filtered message, got %s", rec.Body.String())
	}
	seek := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/subscriptions/filter-sub:seek", strings.NewReader(`{"time":"1970-01-01T00:00:00Z"}`))
	seek.Header.Set("Authorization", auth)
	seek.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, seek)
	if rec.Code != http.StatusOK {
		t.Fatalf("seek: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSecretReplicationAndVersionFilter(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID

	body := `{"replication":{"automatic":{}},"customerManagedEncryption":{"kmsKeyName":"projects/p/locations/global/keyRings/r/cryptoKeys/k"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/secrets?secretId=rep-secret", strings.NewReader(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created["replication"] == nil {
		t.Fatalf("missing replication: %s", rec.Body.String())
	}
	add := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/secrets/rep-secret:addVersion", strings.NewReader(`{"payload":{"data":"`+base64.StdEncoding.EncodeToString([]byte("v1"))+`"}}`))
	add.Header.Set("Authorization", auth)
	add.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, add)
	if rec.Code != http.StatusOK {
		t.Fatalf("addVersion: %d %s", rec.Code, rec.Body.String())
	}
	dis := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/secrets/rep-secret/versions/1:disable", strings.NewReader(`{}`))
	dis.Header.Set("Authorization", auth)
	dis.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, dis)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", rec.Code, rec.Body.String())
	}
	list := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/secrets/rep-secret/versions?filter=state:DISABLED", nil)
	list.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, list)
	if rec.Code != http.StatusOK {
		t.Fatalf("list filter: %d %s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		Versions []struct {
			State string `json:"state"`
		} `json:"versions"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &listResp)
	if len(listResp.Versions) != 1 || listResp.Versions[0].State != "DISABLED" {
		t.Fatalf("filter list = %s", rec.Body.String())
	}
}
