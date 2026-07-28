package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/datastore/apiv1/datastorepb"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestBigQueryInsertAndQueryViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	token := cfg.RootAccessToken
	project := cfg.ProjectID

	createDS := `{"datasetReference":{"datasetId":"labds","projectId":"` + project + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/bigquery/v2/projects/"+project+"/datasets", strings.NewReader(createDS))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create dataset status=%d body=%s", rec.Code, rec.Body.String())
	}

	createT := `{"tableReference":{"tableId":"t1"},"schema":{"fields":[{"name":"name","type":"STRING"}]}}`
	req = httptest.NewRequest(http.MethodPost, "/bigquery/v2/projects/"+project+"/datasets/labds/tables", strings.NewReader(createT))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create table status=%d body=%s", rec.Code, rec.Body.String())
	}

	insert := `{"rows":[{"json":{"name":"alice"}},{"json":{"name":"bob"}}]}`
	req = httptest.NewRequest(http.MethodPost, "/bigquery/v2/projects/"+project+"/datasets/labds/tables/t1/insertAll", strings.NewReader(insert))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insertAll status=%d body=%s", rec.Code, rec.Body.String())
	}

	query := `{"query":"SELECT name FROM labds.t1 WHERE name = 'alice' LIMIT 10"}`
	req = httptest.NewRequest(http.MethodPost, "/bigquery/v2/projects/"+project+"/queries", strings.NewReader(query))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("query status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		TotalRows string `json:"totalRows"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.TotalRows != "1" {
		t.Fatalf("totalRows=%s body=%s", resp.TotalRows, rec.Body.String())
	}
}

func TestFirebaseAuthSignUpSignInViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	body := `{"email":"user@example.com","password":"secret123","returnSecureToken":true,"targetProjectId":"` + cfg.ProjectID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:signUp", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signUp status=%d body=%s", rec.Code, rec.Body.String())
	}
	var signed map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &signed)
	if signed["idToken"] == nil || signed["localId"] == nil {
		t.Fatalf("missing tokens: %#v", signed)
	}

	req = httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:signInWithPassword", strings.NewReader(body))
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signIn status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/identitytoolkit.googleapis.com/v1/projects/"+cfg.ProjectID+"/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMonitoringDescriptorAndTimeSeriesViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	token := cfg.RootAccessToken
	project := cfg.ProjectID
	metricType := "custom.googleapis.com/lab/requests"

	create := `{"type":"` + metricType + `","metricKind":"GAUGE","valueType":"DOUBLE","displayName":"Lab"}`
	req := httptest.NewRequest(http.MethodPost, "/v3/projects/"+project+"/metricDescriptors", strings.NewReader(create))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create descriptor status=%d body=%s", rec.Code, rec.Body.String())
	}

	tsBody := `{
	  "timeSeries":[{
	    "metric":{"type":"` + metricType + `"},
	    "resource":{"type":"global"},
	    "points":[{"interval":{"endTime":"2026-01-01T00:00:00Z"},"value":{"doubleValue":3}},
	              {"interval":{"endTime":"2026-01-01T00:01:00Z"},"value":{"doubleValue":5}}]
	  }]
	}`
	req = httptest.NewRequest(http.MethodPost, "/v3/projects/"+project+"/timeSeries", strings.NewReader(tsBody))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create time series status=%d body=%s", rec.Code, rec.Body.String())
	}

	listURL := "/v3/projects/" + project + "/timeSeries?filter=metric.type%3D%22" + metricType + "%22&aggregation.perSeriesAligner=ALIGN_MEAN"
	req = httptest.NewRequest(http.MethodGet, listURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list time series status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list struct {
		TimeSeries []map[string]any `json:"timeSeries"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.TimeSeries) != 1 {
		t.Fatalf("timeSeries=%#v", list.TimeSeries)
	}
}

func TestDatastorePutLookupQueryViaGRPC(t *testing.T) {
	srv, cfg := testServer(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { _ = http.Serve(ln, srv.Handler()) }()

	conn, err := grpc.NewClient(ln.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := datastorepb.NewDatastoreClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+cfg.RootAccessToken)

	key := &datastorepb.Key{
		PartitionId: &datastorepb.PartitionId{ProjectId: cfg.ProjectID},
		Path: []*datastorepb.Key_PathElement{{
			Kind: "Task", IdType: &datastorepb.Key_PathElement_Name{Name: "t1"},
		}},
	}
	ent := &datastorepb.Entity{
		Key: key,
		Properties: map[string]*datastorepb.Value{
			"title": {ValueType: &datastorepb.Value_StringValue{StringValue: "lab"}},
		},
	}
	_, err = client.Commit(ctx, &datastorepb.CommitRequest{
		ProjectId: cfg.ProjectID,
		Mode:      datastorepb.CommitRequest_NON_TRANSACTIONAL,
		Mutations: []*datastorepb.Mutation{{Operation: &datastorepb.Mutation_Upsert{Upsert: ent}}},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	lookup, err := client.Lookup(ctx, &datastorepb.LookupRequest{
		ProjectId: cfg.ProjectID,
		Keys:      []*datastorepb.Key{key},
	})
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(lookup.Found) != 1 {
		t.Fatalf("found=%v missing=%v", lookup.Found, lookup.Missing)
	}

	qresp, err := client.RunQuery(ctx, &datastorepb.RunQueryRequest{
		ProjectId: cfg.ProjectID,
		QueryType: &datastorepb.RunQueryRequest_Query{
			Query: &datastorepb.Query{
				Kind:  []*datastorepb.KindExpression{{Name: "Task"}},
				Limit: wrapperspb.Int32(10),
				Filter: &datastorepb.Filter{FilterType: &datastorepb.Filter_PropertyFilter{
					PropertyFilter: &datastorepb.PropertyFilter{
						Property: &datastorepb.PropertyReference{Name: "title"},
						Op:       datastorepb.PropertyFilter_EQUAL,
						Value:    &datastorepb.Value{ValueType: &datastorepb.Value_StringValue{StringValue: "lab"}},
					},
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("runQuery: %v", err)
	}
	if len(qresp.GetBatch().GetEntityResults()) != 1 {
		t.Fatalf("query results=%v", qresp.GetBatch().GetEntityResults())
	}
}

func TestEventarcTriggerAndPubSubDelivery(t *testing.T) {
	srv, cfg := testServer(t)
	token := cfg.RootAccessToken
	project := cfg.ProjectID

	received := make(chan []byte, 1)
	hookLN, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer hookLN.Close()
	go func() {
		_ = http.Serve(hookLN, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			select {
			case received <- b:
			default:
			}
			w.WriteHeader(http.StatusOK)
		}))
	}()

	destURI := "http://" + hookLN.Addr().String() + "/hook"
	createTrig := map[string]any{
		"eventFilters": []map[string]string{
			{"attribute": "type", "value": "google.cloud.pubsub.topic.v1.messagePublished"},
		},
		"destination": map[string]any{
			"httpEndpoint": map[string]string{"uri": destURI},
		},
		"transport": map[string]any{
			"pubsub": map[string]string{"topic": "projects/" + project + "/topics/evt-topic"},
		},
	}
	raw, _ := json.Marshal(createTrig)
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/locations/us-central1/triggers?triggerId=t1", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create trigger status=%d body=%s", rec.Code, rec.Body.String())
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { _ = http.Serve(ln, srv.Handler()) }()

	conn, err := grpc.NewClient(ln.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	pub := pubsubpb.NewPublisherClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)

	topic := "projects/" + project + "/topics/evt-topic"
	if _, err := pub.CreateTopic(ctx, &pubsubpb.Topic{Name: topic}); err != nil {
		t.Fatalf("create topic: %v", err)
	}
	if _, err := pub.Publish(ctx, &pubsubpb.PublishRequest{
		Topic: topic,
		Messages: []*pubsubpb.PubsubMessage{{
			Data: []byte("hello-event"),
		}},
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case body := <-received:
		if !bytes.Contains(body, []byte("hello-event")) && !bytes.Contains(body, []byte("messagePublished")) {
			t.Fatalf("unexpected delivery body=%s", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for Eventarc delivery")
	}
}
