package bigquery

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"github.com/google/uuid"
)

const maxInsertAllRows = 500

// Service serves BigQuery REST v2 (lab subset).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers BigQuery REST routes.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /bigquery/v2/projects/{project}/datasets", s.wrap(principalFrom, s.listDatasets))
	mux.HandleFunc("POST /bigquery/v2/projects/{project}/datasets", s.wrap(principalFrom, s.createDataset))
	mux.HandleFunc("GET /bigquery/v2/projects/{project}/datasets/{dataset}", s.wrap(principalFrom, s.getDataset))
	mux.HandleFunc("DELETE /bigquery/v2/projects/{project}/datasets/{dataset}", s.wrap(principalFrom, s.deleteDataset))
	mux.HandleFunc("GET /bigquery/v2/projects/{project}/datasets/{dataset}/tables", s.wrap(principalFrom, s.listTables))
	mux.HandleFunc("POST /bigquery/v2/projects/{project}/datasets/{dataset}/tables", s.wrap(principalFrom, s.createTable))
	mux.HandleFunc("GET /bigquery/v2/projects/{project}/datasets/{dataset}/tables/{table}", s.wrap(principalFrom, s.getTable))
	mux.HandleFunc("DELETE /bigquery/v2/projects/{project}/datasets/{dataset}/tables/{table}", s.wrap(principalFrom, s.deleteTable))
	mux.HandleFunc("POST /bigquery/v2/projects/{project}/datasets/{dataset}/tables/{table}/insertAll", s.wrap(principalFrom, s.insertAll))
	mux.HandleFunc("GET /bigquery/v2/projects/{project}/datasets/{dataset}/tables/{table}/data", s.wrap(principalFrom, s.tabledataList))
	mux.HandleFunc("POST /bigquery/v2/projects/{project}/queries", s.wrap(principalFrom, s.jobsQuery))
	mux.HandleFunc("GET /bigquery/v2/projects/{project}/jobs/{job}", s.wrap(principalFrom, s.jobsGet))
}

type handlerFunc func(w http.ResponseWriter, r *http.Request, p authn.Principal)

func (s *Service) wrap(principalFrom principalFunc, h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := principalFrom(r)
		if !ok {
			gcperrors.Unauthenticated(w, "")
			return
		}
		h(w, r, p)
	}
}

func (s *Service) require(p authn.Principal, permission, projectID string) error {
	ok, err := s.Authz.Evaluate(p.Email, p.IsRoot, permission, "projects/"+projectID)
	if err != nil {
		return err
	}
	if !ok {
		return errDenied
	}
	return nil
}

var errDenied = fmt.Errorf("permission denied")

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func datasetResource(d *store.BQDataset) map[string]any {
	return map[string]any{
		"kind": "bigquery#dataset",
		"id":   d.ProjectID + ":" + d.DatasetID,
		"datasetReference": map[string]string{
			"projectId": d.ProjectID,
			"datasetId": d.DatasetID,
		},
		"location":     d.Location,
		"friendlyName": d.FriendlyName,
		"description":  d.Description,
		"creationTime": strconv.FormatInt(parseMillis(d.CreatedAt), 10),
		"selfLink":     "https://bigquery.googleapis.com/bigquery/v2/projects/" + d.ProjectID + "/datasets/" + d.DatasetID,
	}
}

func tableResource(t *store.BQTable) map[string]any {
	var fields any = []any{}
	_ = json.Unmarshal([]byte(t.SchemaJSON), &fields)
	return map[string]any{
		"kind": "bigquery#table",
		"id":   t.ProjectID + ":" + t.DatasetID + "." + t.TableID,
		"tableReference": map[string]string{
			"projectId": t.ProjectID,
			"datasetId": t.DatasetID,
			"tableId":   t.TableID,
		},
		"friendlyName": t.FriendlyName,
		"description":  t.Description,
		"schema":       map[string]any{"fields": fields},
		"creationTime": strconv.FormatInt(parseMillis(t.CreatedAt), 10),
		"type":         "TABLE",
	}
}

func parseMillis(rfc string) int64 {
	t, err := time.Parse(time.RFC3339Nano, rfc)
	if err != nil {
		t, err = time.Parse(time.RFC3339, rfc)
		if err != nil {
			return 0
		}
	}
	return t.UnixMilli()
}

func (s *Service) createDataset(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "bigquery.datasets.create", project); err != nil {
		writeAuthz(w, err)
		return
	}
	var body struct {
		DatasetReference struct {
			DatasetID string `json:"datasetId"`
			ProjectID string `json:"projectId"`
		} `json:"datasetReference"`
		Location     string `json:"location"`
		FriendlyName string `json:"friendlyName"`
		Description  string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	dsID := body.DatasetReference.DatasetID
	if dsID == "" {
		gcperrors.InvalidArgument(w, "datasetReference.datasetId is required")
		return
	}
	d, created, err := s.Store.CreateBQDataset(store.BQDataset{
		ProjectID: project, DatasetID: dsID, Location: body.Location,
		FriendlyName: body.FriendlyName, Description: body.Description,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "dataset already exists")
		return
	}
	writeJSON(w, http.StatusOK, datasetResource(d))
}

func (s *Service) getDataset(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, dataset := r.PathValue("project"), r.PathValue("dataset")
	if err := s.require(p, "bigquery.datasets.get", project); err != nil {
		writeAuthz(w, err)
		return
	}
	d, ok, err := s.Store.GetBQDataset(project, dataset)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "dataset not found")
		return
	}
	writeJSON(w, http.StatusOK, datasetResource(d))
}

func (s *Service) listDatasets(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "bigquery.datasets.list", project); err != nil {
		writeAuthz(w, err)
		return
	}
	list, err := s.Store.ListBQDatasets(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	datasets := make([]map[string]any, 0, len(list))
	for i := range list {
		d := list[i]
		datasets = append(datasets, map[string]any{
			"kind": "bigquery#dataset",
			"id":   d.ProjectID + ":" + d.DatasetID,
			"datasetReference": map[string]string{
				"projectId": d.ProjectID,
				"datasetId": d.DatasetID,
			},
			"location": d.Location,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "bigquery#datasetList", "datasets": datasets})
}

func (s *Service) deleteDataset(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, dataset := r.PathValue("project"), r.PathValue("dataset")
	if err := s.require(p, "bigquery.datasets.delete", project); err != nil {
		writeAuthz(w, err)
		return
	}
	ok, err := s.Store.DeleteBQDataset(project, dataset)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "dataset not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) createTable(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, dataset := r.PathValue("project"), r.PathValue("dataset")
	if err := s.require(p, "bigquery.tables.create", project); err != nil {
		writeAuthz(w, err)
		return
	}
	var body struct {
		TableReference struct {
			TableID string `json:"tableId"`
		} `json:"tableReference"`
		FriendlyName string `json:"friendlyName"`
		Description  string `json:"description"`
		Schema       *struct {
			Fields json.RawMessage `json:"fields"`
		} `json:"schema"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	tableID := body.TableReference.TableID
	if tableID == "" {
		gcperrors.InvalidArgument(w, "tableReference.tableId is required")
		return
	}
	schemaJSON := "[]"
	if body.Schema != nil && len(body.Schema.Fields) > 0 {
		schemaJSON = string(body.Schema.Fields)
	}
	t, created, err := s.Store.CreateBQTable(store.BQTable{
		ProjectID: project, DatasetID: dataset, TableID: tableID,
		SchemaJSON: schemaJSON, FriendlyName: body.FriendlyName, Description: body.Description,
	})
	if err != nil {
		if strings.Contains(err.Error(), "dataset not found") {
			gcperrors.NotFound(w, "dataset not found")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "table already exists")
		return
	}
	writeJSON(w, http.StatusOK, tableResource(t))
}

func (s *Service) getTable(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, dataset, table := r.PathValue("project"), r.PathValue("dataset"), r.PathValue("table")
	if err := s.require(p, "bigquery.tables.get", project); err != nil {
		writeAuthz(w, err)
		return
	}
	t, ok, err := s.Store.GetBQTable(project, dataset, table)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "table not found")
		return
	}
	writeJSON(w, http.StatusOK, tableResource(t))
}

func (s *Service) listTables(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, dataset := r.PathValue("project"), r.PathValue("dataset")
	if err := s.require(p, "bigquery.tables.list", project); err != nil {
		writeAuthz(w, err)
		return
	}
	list, err := s.Store.ListBQTables(project, dataset)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	tables := make([]map[string]any, 0, len(list))
	for i := range list {
		t := list[i]
		tables = append(tables, map[string]any{
			"kind": "bigquery#table",
			"id":   t.ProjectID + ":" + t.DatasetID + "." + t.TableID,
			"tableReference": map[string]string{
				"projectId": t.ProjectID,
				"datasetId": t.DatasetID,
				"tableId":   t.TableID,
			},
			"type": "TABLE",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "bigquery#tableList", "tables": tables})
}

func (s *Service) deleteTable(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, dataset, table := r.PathValue("project"), r.PathValue("dataset"), r.PathValue("table")
	if err := s.require(p, "bigquery.tables.delete", project); err != nil {
		writeAuthz(w, err)
		return
	}
	ok, err := s.Store.DeleteBQTable(project, dataset, table)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "table not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type schemaField struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Mode string `json:"mode"`
}

func (s *Service) insertAll(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, dataset, table := r.PathValue("project"), r.PathValue("dataset"), r.PathValue("table")
	if err := s.require(p, "bigquery.tables.updateData", project); err != nil {
		writeAuthz(w, err)
		return
	}
	var body struct {
		SkipInvalidRows bool `json:"skipInvalidRows"`
		Rows            []struct {
			InsertID string         `json:"insertId"`
			JSON     map[string]any `json:"json"`
		} `json:"rows"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if len(body.Rows) == 0 {
		gcperrors.InvalidArgument(w, "rows is required")
		return
	}
	if len(body.Rows) > maxInsertAllRows {
		gcperrors.InvalidArgument(w, fmt.Sprintf("at most %d rows per insertAll", maxInsertAllRows))
		return
	}
	tbl, ok, err := s.Store.GetBQTable(project, dataset, table)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "table not found")
		return
	}
	var schema []schemaField
	_ = json.Unmarshal([]byte(tbl.SchemaJSON), &schema)

	rows := make([]map[string]any, 0, len(body.Rows))
	ids := make([]string, 0, len(body.Rows))
	var insertErrors []map[string]any
	for i, row := range body.Rows {
		if row.JSON == nil {
			if body.SkipInvalidRows {
				insertErrors = append(insertErrors, map[string]any{
					"index": i,
					"errors": []map[string]string{{
						"reason": "invalid", "message": "json is required",
					}},
				})
				continue
			}
			gcperrors.InvalidArgument(w, fmt.Sprintf("row %d: json is required", i))
			return
		}
		if errMsg := validateInsertRow(row.JSON, schema); errMsg != "" {
			if body.SkipInvalidRows {
				insertErrors = append(insertErrors, map[string]any{
					"index": i,
					"errors": []map[string]string{{
						"reason": "invalid", "message": errMsg,
					}},
				})
				continue
			}
			gcperrors.InvalidArgument(w, fmt.Sprintf("row %d: %s", i, errMsg))
			return
		}
		rows = append(rows, row.JSON)
		ids = append(ids, row.InsertID)
	}
	if len(rows) > 0 {
		if err := s.Store.InsertBQRows(project, dataset, table, rows, ids); err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
	}
	resp := map[string]any{"kind": "bigquery#tableDataInsertAllResponse"}
	if len(insertErrors) > 0 {
		resp["insertErrors"] = insertErrors
	}
	writeJSON(w, http.StatusOK, resp)
}

func validateInsertRow(row map[string]any, schema []schemaField) string {
	for _, f := range schema {
		if strings.EqualFold(f.Mode, "REQUIRED") {
			v, ok := row[f.Name]
			if !ok || v == nil {
				return "missing required field " + f.Name
			}
		}
	}
	return ""
}

func (s *Service) tabledataList(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, dataset, table := r.PathValue("project"), r.PathValue("dataset"), r.PathValue("table")
	if err := s.require(p, "bigquery.tables.getData", project); err != nil {
		writeAuthz(w, err)
		return
	}
	if _, ok, err := s.Store.GetBQTable(project, dataset, table); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "table not found")
		return
	}
	startIndex, _ := strconv.Atoi(r.URL.Query().Get("startIndex"))
	maxResults := 0
	if v := r.URL.Query().Get("maxResults"); v != "" {
		maxResults, _ = strconv.Atoi(v)
	}
	if maxResults <= 0 {
		maxResults = 1000
	}
	total, err := s.Store.CountBQRows(project, dataset, table)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	rows, err := s.Store.ListBQRowsPage(project, dataset, table, startIndex, maxResults)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	outRows := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		cols := make([]string, 0, len(row))
		for k := range row {
			cols = append(cols, k)
		}
		f := make([]map[string]any, 0, len(cols))
		for _, c := range cols {
			f = append(f, map[string]any{"v": fmt.Sprint(row[c])})
		}
		outRows = append(outRows, map[string]any{"f": f})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":       "bigquery#tableDataList",
		"totalRows":  strconv.Itoa(total),
		"rows":       outRows,
		"startIndex": strconv.Itoa(startIndex),
	})
}

func (s *Service) jobsGet(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, jobID := r.PathValue("project"), r.PathValue("job")
	if err := s.require(p, "bigquery.jobs.get", project); err != nil {
		writeAuthz(w, err)
		return
	}
	j, ok, err := s.Store.GetBQJob(project, jobID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, jobResource(j))
}

func jobResource(j *store.BQJob) map[string]any {
	cfg := map[string]any{
		"jobType": "QUERY",
		"query":   map[string]any{"query": j.Query, "useLegacySql": false},
	}
	status := map[string]any{"state": j.State}
	if j.ErrorJSON != "" {
		var errObj any
		_ = json.Unmarshal([]byte(j.ErrorJSON), &errObj)
		status["errorResult"] = errObj
	}
	return map[string]any{
		"kind": "bigquery#job",
		"id":   j.ProjectID + ":" + j.JobID,
		"jobReference": map[string]string{
			"projectId": j.ProjectID,
			"jobId":     j.JobID,
			"location":  j.Location,
		},
		"configuration": cfg,
		"status":        status,
		"statistics": map[string]any{
			"creationTime": strconv.FormatInt(parseMillis(j.CreatedAt), 10),
			"startTime":    strconv.FormatInt(parseMillis(j.CreatedAt), 10),
			"endTime":      strconv.FormatInt(parseMillis(j.CreatedAt), 10),
		},
	}
}

var (
	reSelect = regexp.MustCompile(`(?is)^\s*SELECT\s+(.+?)\s+FROM\s+([a-zA-Z0-9_]+)\.([a-zA-Z0-9_]+)(?:\s+WHERE\s+([a-zA-Z0-9_]+)\s*=\s*(.+?))?(?:\s+LIMIT\s+(\d+))?\s*$`)
	reJoin   = regexp.MustCompile(`(?is)^\s*SELECT\s+(.+?)\s+FROM\s+([a-zA-Z0-9_]+)\.([a-zA-Z0-9_]+)\s+(?:AS\s+)?([a-zA-Z0-9_]+)\s+JOIN\s+([a-zA-Z0-9_]+)\.([a-zA-Z0-9_]+)\s+(?:AS\s+)?([a-zA-Z0-9_]+)\s+ON\s+([a-zA-Z0-9_]+)\.([a-zA-Z0-9_]+)\s*=\s*([a-zA-Z0-9_]+)\.([a-zA-Z0-9_]+)(?:\s+LIMIT\s+(\d+))?\s*$`)
	reCreate = regexp.MustCompile(`(?is)^\s*CREATE\s+TABLE\s+([a-zA-Z0-9_]+)\.([a-zA-Z0-9_]+)\s*\((.+)\)\s*$`)
	reGroup  = regexp.MustCompile(`(?is)^\s*SELECT\s+([a-zA-Z0-9_]+)\s*,\s*(COUNT\s*\(\s*\*\s*\)|SUM\s*\(\s*([a-zA-Z0-9_]+)\s*\))(?:\s+AS\s+([a-zA-Z0-9_]+))?\s+FROM\s+([a-zA-Z0-9_]+)\.([a-zA-Z0-9_]+)\s+GROUP\s+BY\s+([a-zA-Z0-9_]+)(?:\s+LIMIT\s+(\d+))?\s*$`)
	reInfo   = regexp.MustCompile(`(?is)^\s*SELECT\s+(.+?)\s+FROM\s+([a-zA-Z0-9_]+)\.INFORMATION_SCHEMA\.TABLES\s*$`)
	reUnion  = regexp.MustCompile(`(?is)^(.+?)\s+UNION\s+ALL\s+(.+)$`)
)

func (s *Service) jobsQuery(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "bigquery.jobs.create", project); err != nil {
		writeAuthz(w, err)
		return
	}
	var body struct {
		Query  string `json:"query"`
		DryRun bool   `json:"dryRun"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	q := strings.TrimSpace(strings.TrimSuffix(body.Query, ";"))
	jobID := "lab-" + uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	if m := reCreate.FindStringSubmatch(q); m != nil {
		datasetID, tableID, colsRaw := m[1], m[2], m[3]
		schemaFields, err := parseCreateColumns(colsRaw)
		if err != nil {
			gcperrors.InvalidArgument(w, err.Error())
			return
		}
		schemaJSON, _ := json.Marshal(schemaFields)
		if body.DryRun {
			_ = s.Store.PutBQJob(store.BQJob{
				ProjectID: project, JobID: jobID, Query: q, DryRun: true, State: "DONE", CreatedAt: now,
			})
			writeJSON(w, http.StatusOK, map[string]any{
				"kind":         "bigquery#queryResponse",
				"jobComplete":  true,
				"dryRun":       true,
				"jobReference": map[string]string{"projectId": project, "jobId": jobID},
				"schema":       map[string]any{"fields": schemaFields},
				"totalRows":    "0",
				"rows":         []any{},
			})
			return
		}
		if _, ok, err := s.Store.GetBQDataset(project, datasetID); err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		} else if !ok {
			if _, _, err := s.Store.CreateBQDataset(store.BQDataset{ProjectID: project, DatasetID: datasetID}); err != nil {
				gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
				return
			}
		}
		t, created, err := s.Store.CreateBQTable(store.BQTable{
			ProjectID: project, DatasetID: datasetID, TableID: tableID, SchemaJSON: string(schemaJSON),
		})
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		if !created {
			gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "table already exists")
			return
		}
		_ = t
		_ = s.Store.PutBQJob(store.BQJob{
			ProjectID: project, JobID: jobID, Query: q, State: "DONE", CreatedAt: now,
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"kind":         "bigquery#queryResponse",
			"jobComplete":  true,
			"totalRows":    "0",
			"schema":       map[string]any{"fields": schemaFields},
			"rows":         []any{},
			"jobReference": map[string]string{"projectId": project, "jobId": jobID},
			"numDmlAffectedRows": "0",
		})
		return
	}

	if m := reJoin.FindStringSubmatch(q); m != nil {
		selectCols := m[1]
		dsA, tblA, aliasA := m[2], m[3], m[4]
		dsB, tblB, aliasB := m[5], m[6], m[7]
		leftAlias, leftCol, rightAlias, rightCol := m[8], m[9], m[10], m[11]
		limitStr := m[12]
		if (leftAlias != aliasA && leftAlias != aliasB) || (rightAlias != aliasA && rightAlias != aliasB) {
			gcperrors.InvalidArgument(w, "JOIN ON aliases must match FROM aliases")
			return
		}
		leftRows, err := s.Store.ListBQRows(project, dsA, tblA)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		rightRows, err := s.Store.ListBQRows(project, dsB, tblB)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		joined := joinRows(leftRows, rightRows, aliasA, aliasB, leftAlias, leftCol, rightAlias, rightCol)
		if limitStr != "" {
			lim, _ := strconv.Atoi(limitStr)
			if lim >= 0 && lim < len(joined) {
				joined = joined[:lim]
			}
		}
		cols := parseSelectCols(selectCols, joined)
		resp := buildQueryResponse(project, jobID, cols, joined)
		if body.DryRun {
			resp["dryRun"] = true
			resp["rows"] = []any{}
			resp["totalRows"] = "0"
		}
		raw, _ := json.Marshal(resp)
		_ = s.Store.PutBQJob(store.BQJob{
			ProjectID: project, JobID: jobID, Query: q, DryRun: body.DryRun, State: "DONE",
			ResultJSON: string(raw), CreatedAt: now,
		})
		writeJSON(w, http.StatusOK, resp)
		return
	}

	if m := reInfo.FindStringSubmatch(q); m != nil {
		selectCols, datasetID := m[1], m[2]
		tables, err := s.Store.ListBQTables(project, datasetID)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		rows := make([]map[string]any, 0, len(tables))
		for _, t := range tables {
			rows = append(rows, map[string]any{
				"table_catalog": project,
				"table_schema":  datasetID,
				"table_name":    t.TableID,
				"table_type":    "BASE TABLE",
			})
		}
		cols := parseSelectCols(selectCols, rows)
		if strings.TrimSpace(selectCols) == "*" {
			cols = []string{"table_catalog", "table_schema", "table_name", "table_type"}
		}
		resp := buildQueryResponse(project, jobID, cols, rows)
		if body.DryRun {
			resp["dryRun"] = true
			resp["rows"] = []any{}
			resp["totalRows"] = "0"
		}
		raw, _ := json.Marshal(resp)
		_ = s.Store.PutBQJob(store.BQJob{
			ProjectID: project, JobID: jobID, Query: q, DryRun: body.DryRun, State: "DONE",
			ResultJSON: string(raw), CreatedAt: now,
		})
		writeJSON(w, http.StatusOK, resp)
		return
	}

	if m := reGroup.FindStringSubmatch(q); m != nil {
		groupCol := m[1]
		aggExpr := strings.ToUpper(strings.ReplaceAll(m[2], " ", ""))
		sumCol := m[3]
		alias := m[4]
		datasetID, tableID := m[5], m[6]
		groupByCol := m[7]
		limitStr := m[8]
		if !strings.EqualFold(groupCol, groupByCol) {
			gcperrors.InvalidArgument(w, "GROUP BY column must match the selected grouping column")
			return
		}
		if alias == "" {
			if strings.HasPrefix(aggExpr, "COUNT") {
				alias = "f0_"
			} else {
				alias = "f0_"
			}
		}
		srcRows, err := s.Store.ListBQRows(project, datasetID, tableID)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		type agg struct {
			count int64
			sum   float64
		}
		buckets := map[string]*agg{}
		var order []string
		for _, row := range srcRows {
			key := fmt.Sprint(row[groupByCol])
			a, ok := buckets[key]
			if !ok {
				a = &agg{}
				buckets[key] = a
				order = append(order, key)
			}
			a.count++
			if strings.HasPrefix(aggExpr, "SUM") {
				v, _ := strconv.ParseFloat(fmt.Sprint(row[sumCol]), 64)
				a.sum += v
			}
		}
		outRows := make([]map[string]any, 0, len(order))
		for _, key := range order {
			a := buckets[key]
			val := any(a.count)
			if strings.HasPrefix(aggExpr, "SUM") {
				val = a.sum
			}
			outRows = append(outRows, map[string]any{groupByCol: key, alias: val})
		}
		if limitStr != "" {
			lim, _ := strconv.Atoi(limitStr)
			if lim >= 0 && lim < len(outRows) {
				outRows = outRows[:lim]
			}
		}
		cols := []string{groupByCol, alias}
		resp := buildQueryResponse(project, jobID, cols, outRows)
		if body.DryRun {
			resp["dryRun"] = true
			resp["rows"] = []any{}
			resp["totalRows"] = "0"
		}
		raw, _ := json.Marshal(resp)
		_ = s.Store.PutBQJob(store.BQJob{
			ProjectID: project, JobID: jobID, Query: q, DryRun: body.DryRun, State: "DONE",
			ResultJSON: string(raw), CreatedAt: now,
		})
		writeJSON(w, http.StatusOK, resp)
		return
	}

	if m := reUnion.FindStringSubmatch(q); m != nil {
		leftQ := strings.TrimSpace(m[1])
		rightQ := strings.TrimSpace(m[2])
		leftCols, leftRows, err := s.evalSimpleSelect(project, leftQ)
		if err != nil {
			gcperrors.InvalidArgument(w, "UNION ALL left side: "+err.Error())
			return
		}
		rightCols, rightRows, err := s.evalSimpleSelect(project, rightQ)
		if err != nil {
			gcperrors.InvalidArgument(w, "UNION ALL right side: "+err.Error())
			return
		}
		if len(leftCols) != len(rightCols) {
			gcperrors.InvalidArgument(w, "UNION ALL requires matching column counts")
			return
		}
		combined := append(leftRows, rightRows...)
		resp := buildQueryResponse(project, jobID, leftCols, combined)
		if body.DryRun {
			resp["dryRun"] = true
			resp["rows"] = []any{}
			resp["totalRows"] = "0"
		}
		raw, _ := json.Marshal(resp)
		_ = s.Store.PutBQJob(store.BQJob{
			ProjectID: project, JobID: jobID, Query: q, DryRun: body.DryRun, State: "DONE",
			ResultJSON: string(raw), CreatedAt: now,
		})
		writeJSON(w, http.StatusOK, resp)
		return
	}

	m := reSelect.FindStringSubmatch(q)
	if m == nil {
		gcperrors.InvalidArgument(w, "lab query engine supports: SELECT ... FROM dataset.table [WHERE col = value] [LIMIT n]; JOIN lite; GROUP BY COUNT/SUM; UNION ALL; INFORMATION_SCHEMA.TABLES; CREATE TABLE dataset.table (cols)")
		return
	}
	selectCols, datasetID, tableID := m[1], m[2], m[3]
	whereCol, whereRaw, limitStr := m[4], m[5], m[6]
	rows, err := s.Store.ListBQRows(project, datasetID, tableID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if whereCol != "" {
		want := strings.TrimSpace(whereRaw)
		want = strings.Trim(want, `"'`)
		filtered := rows[:0]
		for _, row := range rows {
			got, ok := row[whereCol]
			if !ok {
				continue
			}
			if fmt.Sprint(got) == want {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	if limitStr != "" {
		lim, _ := strconv.Atoi(limitStr)
		if lim >= 0 && lim < len(rows) {
			rows = rows[:lim]
		}
	}
	cols := parseSelectCols(selectCols, rows)
	resp := buildQueryResponse(project, jobID, cols, rows)
	if body.DryRun {
		resp["dryRun"] = true
		resp["rows"] = []any{}
		resp["totalRows"] = "0"
	}
	raw, _ := json.Marshal(resp)
	_ = s.Store.PutBQJob(store.BQJob{
		ProjectID: project, JobID: jobID, Query: q, DryRun: body.DryRun, State: "DONE",
		ResultJSON: string(raw), CreatedAt: now,
	})
	writeJSON(w, http.StatusOK, resp)
}

// evalSimpleSelect runs a lab SELECT (no JOIN/GROUP/UNION) and returns columns + rows.
func (s *Service) evalSimpleSelect(project, q string) ([]string, []map[string]any, error) {
	m := reSelect.FindStringSubmatch(strings.TrimSpace(q))
	if m == nil {
		return nil, nil, fmt.Errorf("unsupported SELECT")
	}
	selectCols, datasetID, tableID := m[1], m[2], m[3]
	whereCol, whereRaw, limitStr := m[4], m[5], m[6]
	rows, err := s.Store.ListBQRows(project, datasetID, tableID)
	if err != nil {
		return nil, nil, err
	}
	if whereCol != "" {
		want := strings.TrimSpace(whereRaw)
		want = strings.Trim(want, `"'`)
		filtered := rows[:0]
		for _, row := range rows {
			got, ok := row[whereCol]
			if !ok {
				continue
			}
			if fmt.Sprint(got) == want {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	if limitStr != "" {
		lim, _ := strconv.Atoi(limitStr)
		if lim >= 0 && lim < len(rows) {
			rows = rows[:lim]
		}
	}
	cols := parseSelectCols(selectCols, rows)
	projected := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out := map[string]any{}
		for _, c := range cols {
			out[c] = row[c]
		}
		projected = append(projected, out)
	}
	return cols, projected, nil
}

func parseCreateColumns(raw string) ([]map[string]any, error) {
	parts := strings.Split(raw, ",")
	out := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		fields := strings.Fields(p)
		if len(fields) < 2 {
			return nil, fmt.Errorf("invalid column definition %q", p)
		}
		mode := "NULLABLE"
		if len(fields) >= 3 {
			mode = strings.ToUpper(fields[2])
		}
		out = append(out, map[string]any{
			"name": fields[0],
			"type": strings.ToUpper(fields[1]),
			"mode": mode,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("CREATE TABLE requires at least one column")
	}
	return out, nil
}

func joinRows(left, right []map[string]any, aliasA, aliasB, leftAlias, leftCol, rightAlias, rightCol string) []map[string]any {
	var out []map[string]any
	for _, lr := range left {
		for _, rr := range right {
			var leftVal, rightVal any
			if leftAlias == aliasA {
				leftVal = lr[leftCol]
			} else {
				leftVal = rr[leftCol]
			}
			if rightAlias == aliasB {
				rightVal = rr[rightCol]
			} else {
				rightVal = lr[rightCol]
			}
			if fmt.Sprint(leftVal) != fmt.Sprint(rightVal) {
				continue
			}
			merged := map[string]any{}
			for k, v := range lr {
				merged[aliasA+"."+k] = v
				merged[k] = v
			}
			for k, v := range rr {
				merged[aliasB+"."+k] = v
				if _, exists := merged[k]; !exists {
					merged[k] = v
				}
			}
			out = append(out, merged)
		}
	}
	return out
}

func parseSelectCols(selectCols string, rows []map[string]any) []string {
	if strings.TrimSpace(selectCols) == "*" {
		seen := map[string]bool{}
		var cols []string
		for _, row := range rows {
			for k := range row {
				if strings.Contains(k, ".") {
					continue
				}
				if !seen[k] {
					seen[k] = true
					cols = append(cols, k)
				}
			}
		}
		return cols
	}
	var cols []string
	for _, c := range strings.Split(selectCols, ",") {
		cols = append(cols, strings.TrimSpace(c))
	}
	return cols
}

func buildQueryResponse(project, jobID string, cols []string, rows []map[string]any) map[string]any {
	fields := make([]map[string]any, 0, len(cols))
	for _, c := range cols {
		fields = append(fields, map[string]any{"name": c, "type": "STRING", "mode": "NULLABLE"})
	}
	outRows := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		f := make([]map[string]any, 0, len(cols))
		for _, c := range cols {
			f = append(f, map[string]any{"v": fmt.Sprint(row[c])})
		}
		outRows = append(outRows, map[string]any{"f": f})
	}
	return map[string]any{
		"kind":         "bigquery#queryResponse",
		"jobComplete":  true,
		"totalRows":    strconv.Itoa(len(outRows)),
		"schema":       map[string]any{"fields": fields},
		"rows":         outRows,
		"jobReference": map[string]string{"projectId": project, "jobId": jobID},
	}
}

func writeAuthz(w http.ResponseWriter, err error) {
	if err == errDenied {
		gcperrors.PermissionDenied(w, "")
		return
	}
	gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
}
