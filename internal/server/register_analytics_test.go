package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

	sumURL := "/v3/projects/" + project + "/timeSeries?filter=metric.type%3D%22" + metricType + "%22&aggregation.perSeriesAligner=ALIGN_SUM"
	req = httptest.NewRequest(http.MethodGet, sumURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list ALIGN_SUM status=%d body=%s", rec.Code, rec.Body.String())
	}

	getDesc := "/v3/projects/" + project + "/metricDescriptors/" + metricType
	req = httptest.NewRequest(http.MethodGet, getDesc, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get descriptor status=%d body=%s", rec.Code, rec.Body.String())
	}

	delURL := "/v3/projects/" + project + "/timeSeries:delete?filter=metric.type%3D%22" + metricType + "%22"
	req = httptest.NewRequest(http.MethodPost, delURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete time series status=%d body=%s", rec.Code, rec.Body.String())
	}

	createPol := `{"displayName":"Lab alert","combiner":"OR","conditions":[{"displayName":"c1"}],"enabled":true}`
	req = httptest.NewRequest(http.MethodPost, "/v3/projects/"+project+"/alertPolicies?alertPolicyId=pol1", strings.NewReader(createPol))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create alert policy status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v3/projects/"+project+"/alertPolicies/pol1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get alert policy status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPatch, "/v3/projects/"+project+"/alertPolicies/pol1", strings.NewReader(`{"displayName":"Renamed","enabled":false}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch alert policy status=%d body=%s", rec.Code, rec.Body.String())
	}
	var pol map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &pol)
	if pol["displayName"] != "Renamed" || pol["enabled"] != false {
		t.Fatalf("patched=%#v", pol)
	}

	req = httptest.NewRequest(http.MethodGet, "/v3/projects/"+project+"/alertPolicies", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list alert policies status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, "/v3/projects/"+project+"/alertPolicies/pol1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete alert policy status=%d", rec.Code)
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

func TestDatastoreGQLAllocateIdsAndTransactions(t *testing.T) {
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

	for _, name := range []string{"g1", "g2"} {
		key := &datastorepb.Key{
			PartitionId: &datastorepb.PartitionId{ProjectId: cfg.ProjectID},
			Path: []*datastorepb.Key_PathElement{{
				Kind: "Item", IdType: &datastorepb.Key_PathElement_Name{Name: name},
			}},
		}
		ent := &datastorepb.Entity{
			Key: key,
			Properties: map[string]*datastorepb.Value{
				"color": {ValueType: &datastorepb.Value_StringValue{StringValue: "red"}},
				"size":  {ValueType: &datastorepb.Value_IntegerValue{IntegerValue: 1}},
			},
		}
		if name == "g2" {
			ent.Properties["color"] = &datastorepb.Value{ValueType: &datastorepb.Value_StringValue{StringValue: "blue"}}
		}
		if _, err := client.Commit(ctx, &datastorepb.CommitRequest{
			ProjectId: cfg.ProjectID,
			Mode:      datastorepb.CommitRequest_NON_TRANSACTIONAL,
			Mutations: []*datastorepb.Mutation{{Operation: &datastorepb.Mutation_Upsert{Upsert: ent}}},
		}); err != nil {
			t.Fatalf("commit %s: %v", name, err)
		}
	}

	gql, err := client.RunQuery(ctx, &datastorepb.RunQueryRequest{
		ProjectId: cfg.ProjectID,
		QueryType: &datastorepb.RunQueryRequest_GqlQuery{
			GqlQuery: &datastorepb.GqlQuery{
				QueryString:   "SELECT * FROM Item WHERE color = 'red' AND size = 1 LIMIT 5",
				AllowLiterals: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("gql: %v", err)
	}
	if len(gql.GetBatch().GetEntityResults()) != 1 {
		t.Fatalf("gql results=%v", gql.GetBatch().GetEntityResults())
	}

	andResp, err := client.RunQuery(ctx, &datastorepb.RunQueryRequest{
		ProjectId: cfg.ProjectID,
		QueryType: &datastorepb.RunQueryRequest_Query{
			Query: &datastorepb.Query{
				Kind: []*datastorepb.KindExpression{{Name: "Item"}},
				Filter: &datastorepb.Filter{FilterType: &datastorepb.Filter_CompositeFilter{
					CompositeFilter: &datastorepb.CompositeFilter{
						Op: datastorepb.CompositeFilter_AND,
						Filters: []*datastorepb.Filter{
							{FilterType: &datastorepb.Filter_PropertyFilter{PropertyFilter: &datastorepb.PropertyFilter{
								Property: &datastorepb.PropertyReference{Name: "color"},
								Op:       datastorepb.PropertyFilter_EQUAL,
								Value:    &datastorepb.Value{ValueType: &datastorepb.Value_StringValue{StringValue: "blue"}},
							}}},
							{FilterType: &datastorepb.Filter_PropertyFilter{PropertyFilter: &datastorepb.PropertyFilter{
								Property: &datastorepb.PropertyReference{Name: "size"},
								Op:       datastorepb.PropertyFilter_EQUAL,
								Value:    &datastorepb.Value{ValueType: &datastorepb.Value_IntegerValue{IntegerValue: 1}},
							}}},
						},
					},
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("and query: %v", err)
	}
	if len(andResp.GetBatch().GetEntityResults()) != 1 {
		t.Fatalf("and results=%v", andResp.GetBatch().GetEntityResults())
	}

	alloc, err := client.AllocateIds(ctx, &datastorepb.AllocateIdsRequest{
		ProjectId: cfg.ProjectID,
		Keys: []*datastorepb.Key{{
			PartitionId: &datastorepb.PartitionId{ProjectId: cfg.ProjectID},
			Path:        []*datastorepb.Key_PathElement{{Kind: "Auto"}},
		}},
	})
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if len(alloc.GetKeys()) != 1 || alloc.GetKeys()[0].Path[0].GetId() == 0 {
		t.Fatalf("alloc=%v", alloc.GetKeys())
	}

	begun, err := client.BeginTransaction(ctx, &datastorepb.BeginTransactionRequest{ProjectId: cfg.ProjectID})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	txKey := &datastorepb.Key{
		PartitionId: &datastorepb.PartitionId{ProjectId: cfg.ProjectID},
		Path: []*datastorepb.Key_PathElement{{
			Kind: "Tx", IdType: &datastorepb.Key_PathElement_Name{Name: "one"},
		}},
	}
	_, err = client.Commit(ctx, &datastorepb.CommitRequest{
		ProjectId: cfg.ProjectID,
		Mode:      datastorepb.CommitRequest_TRANSACTIONAL,
		TransactionSelector: &datastorepb.CommitRequest_Transaction{
			Transaction: begun.GetTransaction(),
		},
		Mutations: []*datastorepb.Mutation{{Operation: &datastorepb.Mutation_Upsert{Upsert: &datastorepb.Entity{
			Key: txKey,
			Properties: map[string]*datastorepb.Value{
				"ok": {ValueType: &datastorepb.Value_BooleanValue{BooleanValue: true}},
			},
		}}}},
	})
	if err != nil {
		t.Fatalf("tx commit: %v", err)
	}
	_, err = client.Commit(ctx, &datastorepb.CommitRequest{
		ProjectId: cfg.ProjectID,
		Mode:      datastorepb.CommitRequest_TRANSACTIONAL,
		TransactionSelector: &datastorepb.CommitRequest_Transaction{
			Transaction: begun.GetTransaction(),
		},
	})
	if err == nil {
		t.Fatal("expected reused transaction to fail")
	}

	begun2, err := client.BeginTransaction(ctx, &datastorepb.BeginTransactionRequest{ProjectId: cfg.ProjectID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Rollback(ctx, &datastorepb.RollbackRequest{
		ProjectId: cfg.ProjectID, Transaction: begun2.GetTransaction(),
	}); err != nil {
		t.Fatalf("rollback: %v", err)
	}
}

func TestBigQueryCreateJoinDryRunJobsAndSkipInvalid(t *testing.T) {
	srv, cfg := testServer(t)
	token := cfg.RootAccessToken
	project := cfg.ProjectID

	createDS := `{"datasetReference":{"datasetId":"joinlab","projectId":"` + project + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/bigquery/v2/projects/"+project+"/datasets", strings.NewReader(createDS))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create dataset status=%d body=%s", rec.Code, rec.Body.String())
	}

	createSQL := `{"query":"CREATE TABLE joinlab.orders (id STRING REQUIRED, user_id STRING)"}`
	req = httptest.NewRequest(http.MethodPost, "/bigquery/v2/projects/"+project+"/queries", strings.NewReader(createSQL))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create table query status=%d body=%s", rec.Code, rec.Body.String())
	}
	var createResp struct {
		JobReference struct {
			JobID string `json:"jobId"`
		} `json:"jobReference"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &createResp)
	if createResp.JobReference.JobID == "" {
		t.Fatalf("missing job id: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/bigquery/v2/projects/"+project+"/jobs/"+createResp.JobReference.JobID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("jobs.get status=%d body=%s", rec.Code, rec.Body.String())
	}

	createUsers := `{"tableReference":{"tableId":"users"},"schema":{"fields":[{"name":"id","type":"STRING","mode":"REQUIRED"},{"name":"name","type":"STRING"}]}}`
	req = httptest.NewRequest(http.MethodPost, "/bigquery/v2/projects/"+project+"/datasets/joinlab/tables", strings.NewReader(createUsers))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create users status=%d body=%s", rec.Code, rec.Body.String())
	}

	insertUsers := `{"rows":[{"json":{"id":"u1","name":"Ada"}},{"json":{"name":"no-id"}}],"skipInvalidRows":true}`
	req = httptest.NewRequest(http.MethodPost, "/bigquery/v2/projects/"+project+"/datasets/joinlab/tables/users/insertAll", strings.NewReader(insertUsers))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insertAll skip status=%d body=%s", rec.Code, rec.Body.String())
	}
	var insertResp struct {
		InsertErrors []any `json:"insertErrors"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &insertResp)
	if len(insertResp.InsertErrors) != 1 {
		t.Fatalf("insertErrors=%v body=%s", insertResp.InsertErrors, rec.Body.String())
	}

	insertOrders := `{"rows":[{"json":{"id":"o1","user_id":"u1"}}]}`
	req = httptest.NewRequest(http.MethodPost, "/bigquery/v2/projects/"+project+"/datasets/joinlab/tables/orders/insertAll", strings.NewReader(insertOrders))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert orders status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/bigquery/v2/projects/"+project+"/datasets/joinlab/tables/users/data?maxResults=10", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tabledata.list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		TotalRows string `json:"totalRows"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &listResp)
	if listResp.TotalRows != "1" {
		t.Fatalf("tabledata totalRows=%s body=%s", listResp.TotalRows, rec.Body.String())
	}

	dry := `{"query":"SELECT name FROM joinlab.users WHERE name = 'Ada'","dryRun":true}`
	req = httptest.NewRequest(http.MethodPost, "/bigquery/v2/projects/"+project+"/queries", strings.NewReader(dry))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("dryRun status=%d body=%s", rec.Code, rec.Body.String())
	}
	var dryResp struct {
		TotalRows string `json:"totalRows"`
		DryRun    bool   `json:"dryRun"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &dryResp)
	if !dryResp.DryRun || dryResp.TotalRows != "0" {
		t.Fatalf("dryRun resp=%#v body=%s", dryResp, rec.Body.String())
	}

	joinQ := `{"query":"SELECT a.name, b.id FROM joinlab.users a JOIN joinlab.orders b ON a.id = b.user_id LIMIT 10"}`
	req = httptest.NewRequest(http.MethodPost, "/bigquery/v2/projects/"+project+"/queries", strings.NewReader(joinQ))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("join status=%d body=%s", rec.Code, rec.Body.String())
	}
	var joinResp struct {
		TotalRows string `json:"totalRows"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &joinResp)
	if joinResp.TotalRows != "1" {
		t.Fatalf("join totalRows=%s body=%s", joinResp.TotalRows, rec.Body.String())
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

func TestEventarcChannelAndAttributeFilters(t *testing.T) {
	srv, cfg := testServer(t)
	token := cfg.RootAccessToken
	project := cfg.ProjectID

	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/locations/us-central1/channels?channelId=ch1",
		strings.NewReader(`{"provider":"//storage.googleapis.com","pubsubTopic":"projects/`+project+`/topics/ch"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create channel status=%d body=%s", rec.Code, rec.Body.String())
	}

	createTrig := map[string]any{
		"eventFilters": []map[string]any{
			{"attribute": "type", "value": "google.cloud.pubsub.topic.v1.messagePublished"},
			{"attribute": "custom", "value": "yes"},
			{"attribute": "map", "values": map[string]string{"env": "lab"}},
		},
		"destination": map[string]any{"httpEndpoint": map[string]string{"uri": "http://127.0.0.1:9/noop"}},
		"channel":     "projects/" + project + "/locations/us-central1/channels/ch1",
	}
	raw, _ := json.Marshal(createTrig)
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/locations/us-central1/triggers?triggerId=tfilt", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create trigger status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("channels/ch1")) {
		t.Fatalf("expected channel on trigger: %s", rec.Body.String())
	}
}

func TestFirebaseAuthOOBClaimsVerifyPagination(t *testing.T) {
	srv, cfg := testServer(t)
	token := cfg.RootAccessToken
	project := cfg.ProjectID

	for i := 0; i < 3; i++ {
		body := fmt.Sprintf(`{"email":"u%d@example.com","password":"secret123","returnSecureToken":true,"targetProjectId":"%s"}`, i, project)
		req := httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:signUp", strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("signUp %d status=%d body=%s", i, rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/identitytoolkit.googleapis.com/v1/projects/"+project+"/accounts?maxResults=2", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d", rec.Code)
	}
	var page struct {
		Users         []map[string]any `json:"users"`
		NextPageToken string           `json:"nextPageToken"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	if len(page.Users) != 2 || page.NextPageToken == "" {
		t.Fatalf("page=%#v", page)
	}

	var first map[string]any
	signup := `{"email":"claims@example.com","password":"secret123","returnSecureToken":true,"targetProjectId":"` + project + `"}`
	req = httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:signUp", strings.NewReader(signup))
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &first)
	localID, _ := first["localId"].(string)
	idToken, _ := first["idToken"].(string)

	claimsBody := `{"localId":"` + localID + `","customAttributes":{"role":"admin"}}`
	req = httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/projects/"+project+"/accounts:setCustomUserClaims", strings.NewReader(claimsBody))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("admin")) {
		t.Fatalf("setClaims status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/projects/"+project+"/accounts:verifyIdToken",
		strings.NewReader(`{"idToken":"`+idToken+`"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"valid":true`)) {
		t.Fatalf("verify status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:sendOobCode",
		strings.NewReader(`{"requestType":"PASSWORD_RESET","email":"claims@example.com","targetProjectId":"`+project+`"}`))
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sendOob status=%d body=%s", rec.Code, rec.Body.String())
	}
	var oob map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &oob)
	code, _ := oob["oobCode"].(string)
	if code == "" {
		t.Fatalf("missing oobCode: %#v", oob)
	}
	req = httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:resetPassword",
		strings.NewReader(`{"oobCode":"`+code+`","newPassword":"newsecret456"}`))
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resetPassword status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/identitytoolkit.googleapis.com/v1/accounts:signInWithPassword",
		strings.NewReader(`{"email":"claims@example.com","password":"newsecret456","returnSecureToken":true,"targetProjectId":"`+project+`"}`))
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signIn after reset status=%d body=%s", rec.Code, rec.Body.String())
	}
}

