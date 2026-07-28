package monitoring

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// Service serves Cloud Monitoring MetricService REST v3 (lab subset).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Monitoring MetricService routes.
// Metric type paths contain slashes; captured via {rest...}.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("POST /v3/projects/{project}/metricDescriptors", s.wrap(principalFrom, s.createDescriptor))
	mux.HandleFunc("GET /v3/projects/{project}/metricDescriptors", s.wrap(principalFrom, s.listDescriptors))
	mux.HandleFunc("GET /v3/projects/{project}/metricDescriptors/{rest...}", s.wrap(principalFrom, s.getDescriptor))
	mux.HandleFunc("POST /v3/projects/{project}/timeSeries", s.wrap(principalFrom, s.createTimeSeries))
	mux.HandleFunc("GET /v3/projects/{project}/timeSeries", s.wrap(principalFrom, s.listTimeSeries))
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

func descriptorResource(d *store.MetricDescriptorRow) map[string]any {
	var labels any = []any{}
	_ = json.Unmarshal([]byte(d.LabelsJSON), &labels)
	return map[string]any{
		"name":        d.Name,
		"type":        d.Type,
		"metricKind":  d.MetricKind,
		"valueType":   d.ValueType,
		"description": d.Description,
		"displayName": d.DisplayName,
		"labels":      labels,
	}
}

func (s *Service) createDescriptor(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "monitoring.metricDescriptors.create", project); err != nil {
		writeAuthz(w, err)
		return
	}
	var body struct {
		Type        string          `json:"type"`
		MetricKind  string          `json:"metricKind"`
		ValueType   string          `json:"valueType"`
		Description string          `json:"description"`
		DisplayName string          `json:"displayName"`
		Labels      json.RawMessage `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if body.Type == "" {
		gcperrors.InvalidArgument(w, "type is required")
		return
	}
	labelsJSON := "[]"
	if len(body.Labels) > 0 {
		labelsJSON = string(body.Labels)
	}
	d, created, err := s.Store.CreateMetricDescriptor(store.MetricDescriptorRow{
		ProjectID: project, Type: body.Type, MetricKind: body.MetricKind, ValueType: body.ValueType,
		Description: body.Description, DisplayName: body.DisplayName, LabelsJSON: labelsJSON,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "metric descriptor already exists")
		return
	}
	writeJSON(w, http.StatusOK, descriptorResource(d))
}

func (s *Service) getDescriptor(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	rest := strings.Trim(r.PathValue("rest"), "/")
	if err := s.require(p, "monitoring.metricDescriptors.get", project); err != nil {
		writeAuthz(w, err)
		return
	}
	name := "projects/" + project + "/metricDescriptors/" + rest
	d, ok, err := s.Store.GetMetricDescriptor(project, name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		d, ok, err = s.Store.GetMetricDescriptor(project, rest)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
	}
	if !ok {
		gcperrors.NotFound(w, "metric descriptor not found")
		return
	}
	writeJSON(w, http.StatusOK, descriptorResource(d))
}

func (s *Service) listDescriptors(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "monitoring.metricDescriptors.list", project); err != nil {
		writeAuthz(w, err)
		return
	}
	list, err := s.Store.ListMetricDescriptors(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, descriptorResource(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"metricDescriptors": out})
}

func (s *Service) createTimeSeries(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "monitoring.timeSeries.create", project); err != nil {
		writeAuthz(w, err)
		return
	}
	var body struct {
		TimeSeries []struct {
			Metric *struct {
				Type   string            `json:"type"`
				Labels map[string]string `json:"labels"`
			} `json:"metric"`
			Resource *struct {
				Type   string            `json:"type"`
				Labels map[string]string `json:"labels"`
			} `json:"resource"`
			Points []struct {
				Interval *struct {
					EndTime   string `json:"endTime"`
					StartTime string `json:"startTime"`
				} `json:"interval"`
				Value json.RawMessage `json:"value"`
			} `json:"points"`
		} `json:"timeSeries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if len(body.TimeSeries) == 0 {
		gcperrors.InvalidArgument(w, "timeSeries is required")
		return
	}
	var points []store.TimeSeriesPoint
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, ts := range body.TimeSeries {
		if ts.Metric == nil || ts.Metric.Type == "" {
			gcperrors.InvalidArgument(w, "metric.type is required")
			return
		}
		resType := "global"
		resLabels := map[string]string{}
		if ts.Resource != nil {
			if ts.Resource.Type != "" {
				resType = ts.Resource.Type
			}
			if ts.Resource.Labels != nil {
				resLabels = ts.Resource.Labels
			}
		}
		metricLabels := map[string]string{}
		if ts.Metric.Labels != nil {
			metricLabels = ts.Metric.Labels
		}
		resLabelsJSON, _ := json.Marshal(resLabels)
		metricLabelsJSON, _ := json.Marshal(metricLabels)
		for _, pt := range ts.Points {
			endTime := now
			startTime := ""
			if pt.Interval != nil {
				if pt.Interval.EndTime != "" {
					endTime = pt.Interval.EndTime
				}
				startTime = pt.Interval.StartTime
			}
			val := "{}"
			if len(pt.Value) > 0 {
				val = string(pt.Value)
			}
			points = append(points, store.TimeSeriesPoint{
				ProjectID: project, MetricType: ts.Metric.Type, ResourceType: resType,
				ResourceLabelsJSON: string(resLabelsJSON), MetricLabelsJSON: string(metricLabelsJSON),
				EndTime: endTime, StartTime: startTime, ValueJSON: val,
			})
		}
	}
	if err := s.Store.CreateTimeSeriesPoints(points); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func (s *Service) listTimeSeries(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "monitoring.timeSeries.list", project); err != nil {
		writeAuthz(w, err)
		return
	}
	filter := r.URL.Query().Get("filter")
	metricType := parseMetricTypeFilter(filter)
	intervalStart := r.URL.Query().Get("interval.startTime")
	intervalEnd := r.URL.Query().Get("interval.endTime")
	alignment := r.URL.Query().Get("aggregation.perSeriesAligner")
	points, err := s.Store.ListTimeSeriesPoints(store.ListTimeSeriesFilter{
		ProjectID: project, MetricType: metricType, StartTime: intervalStart, EndTime: intervalEnd,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}

	type seriesKey struct {
		MetricType   string
		ResourceType string
		ResLabels    string
		MetricLabels string
	}
	grouped := map[seriesKey][]store.TimeSeriesPoint{}
	for _, pt := range points {
		k := seriesKey{pt.MetricType, pt.ResourceType, pt.ResourceLabelsJSON, pt.MetricLabelsJSON}
		grouped[k] = append(grouped[k], pt)
	}

	out := []map[string]any{}
	for k, pts := range grouped {
		var resLabels, metricLabels map[string]string
		_ = json.Unmarshal([]byte(k.ResLabels), &resLabels)
		_ = json.Unmarshal([]byte(k.MetricLabels), &metricLabels)
		aligned := alignPoints(pts, alignment)
		pointObjs := make([]map[string]any, 0, len(aligned))
		for _, pt := range aligned {
			var val any
			_ = json.Unmarshal([]byte(pt.ValueJSON), &val)
			pointObjs = append(pointObjs, map[string]any{
				"interval": map[string]string{"endTime": pt.EndTime, "startTime": pt.StartTime},
				"value":    val,
			})
		}
		out = append(out, map[string]any{
			"metric":   map[string]any{"type": k.MetricType, "labels": metricLabels},
			"resource": map[string]any{"type": k.ResourceType, "labels": resLabels},
			"points":   pointObjs,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"timeSeries": out})
}

func parseMetricTypeFilter(filter string) string {
	// metric.type = "custom.googleapis.com/foo"
	filter = strings.TrimSpace(filter)
	const prefix = `metric.type=`
	if !strings.Contains(filter, "metric.type") {
		return ""
	}
	i := strings.Index(filter, "metric.type")
	rest := strings.TrimSpace(filter[i+len("metric.type"):])
	rest = strings.TrimPrefix(rest, "=")
	rest = strings.TrimSpace(rest)
	rest = strings.Trim(rest, `"`)
	_ = prefix
	return rest
}

func alignPoints(pts []store.TimeSeriesPoint, aligner string) []store.TimeSeriesPoint {
	aligner = strings.ToUpper(aligner)
	if aligner == "" || aligner == "ALIGN_NONE" || len(pts) == 0 {
		return pts
	}
	switch aligner {
	case "ALIGN_MEAN", "ALIGN_SUM", "ALIGN_MAX", "ALIGN_MIN":
		var vals []float64
		for _, pt := range pts {
			var v map[string]any
			if err := json.Unmarshal([]byte(pt.ValueJSON), &v); err != nil {
				continue
			}
			if dv, ok := v["doubleValue"]; ok {
				vals = append(vals, toFloat(dv))
			} else if iv, ok := v["int64Value"]; ok {
				vals = append(vals, toFloat(iv))
			}
		}
		if len(vals) == 0 {
			return pts[:1]
		}
		var agg float64
		switch aligner {
		case "ALIGN_SUM":
			for _, v := range vals {
				agg += v
			}
		case "ALIGN_MEAN":
			for _, v := range vals {
				agg += v
			}
			agg /= float64(len(vals))
		case "ALIGN_MAX":
			agg = vals[0]
			for _, v := range vals[1:] {
				if v > agg {
					agg = v
				}
			}
		case "ALIGN_MIN":
			agg = vals[0]
			for _, v := range vals[1:] {
				if v < agg {
					agg = v
				}
			}
		}
		last := pts[len(pts)-1]
		raw, _ := json.Marshal(map[string]any{"doubleValue": agg})
		last.ValueJSON = string(raw)
		return []store.TimeSeriesPoint{last}
	default:
		return pts
	}
}

func toFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	case json.Number:
		f, _ := t.Float64()
		return f
	default:
		f, _ := strconv.ParseFloat(fmt.Sprint(v), 64)
		return f
	}
}

func writeAuthz(w http.ResponseWriter, err error) {
	if err == errDenied {
		gcperrors.PermissionDenied(w, "")
		return
	}
	gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
}
