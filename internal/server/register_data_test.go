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
