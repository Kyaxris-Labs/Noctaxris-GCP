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
)

const maxInsertAllRows = 500

// Service serves BigQuery REST v2 (lab subset).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers BigQuery REST routes.
// Colon methods (jobs.query uses /queries) are path-based; insertAll is a path segment.
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
	mux.HandleFunc("POST /bigquery/v2/projects/{project}/queries", s.wrap(principalFrom, s.jobsQuery))
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

func (s *Service) insertAll(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, dataset, table := r.PathValue("project"), r.PathValue("dataset"), r.PathValue("table")
	if err := s.require(p, "bigquery.tables.updateData", project); err != nil {
		writeAuthz(w, err)
		return
	}
	var body struct {
		Rows []struct {
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
	rows := make([]map[string]any, 0, len(body.Rows))
	ids := make([]string, 0, len(body.Rows))
	for _, row := range body.Rows {
		if row.JSON == nil {
			row.JSON = map[string]any{}
		}
		rows = append(rows, row.JSON)
		ids = append(ids, row.InsertID)
	}
	if err := s.Store.InsertBQRows(project, dataset, table, rows, ids); err != nil {
		if strings.Contains(err.Error(), "table not found") {
			gcperrors.NotFound(w, "table not found")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "bigquery#tableDataInsertAllResponse"})
}

var (
	reSelect = regexp.MustCompile(`(?is)^\s*SELECT\s+(.+?)\s+FROM\s+([a-zA-Z0-9_]+)\.([a-zA-Z0-9_]+)(?:\s+WHERE\s+([a-zA-Z0-9_]+)\s*=\s*(.+?))?(?:\s+LIMIT\s+(\d+))?\s*$`)
)

func (s *Service) jobsQuery(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "bigquery.jobs.create", project); err != nil {
		writeAuthz(w, err)
		return
	}
	var body struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	q := strings.TrimSpace(strings.TrimSuffix(body.Query, ";"))
	m := reSelect.FindStringSubmatch(q)
	if m == nil {
		gcperrors.InvalidArgument(w, "lab query engine supports: SELECT ... FROM dataset.table [WHERE col = value] [LIMIT n]")
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
	cols := []string{}
	if strings.TrimSpace(selectCols) == "*" {
		seen := map[string]bool{}
		for _, row := range rows {
			for k := range row {
				if !seen[k] {
					seen[k] = true
					cols = append(cols, k)
				}
			}
		}
	} else {
		for _, c := range strings.Split(selectCols, ",") {
			cols = append(cols, strings.TrimSpace(c))
		}
	}
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
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":             "bigquery#queryResponse",
		"jobComplete":      true,
		"totalRows":        strconv.Itoa(len(outRows)),
		"schema":           map[string]any{"fields": fields},
		"rows":             outRows,
		"jobReference":     map[string]string{"projectId": project, "jobId": "lab-query"},
	})
}

func writeAuthz(w http.ResponseWriter, err error) {
	if err == errDenied {
		gcperrors.PermissionDenied(w, "")
		return
	}
	gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
}
