package dns

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// Service serves Cloud DNS REST v1 (managedZones + resourceRecordSets CRUD).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Cloud DNS dns/v1 REST routes.
// Colon methods on zone/rrset segments are parsed via splitAction (none required for this cut).
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /dns/v1/projects/{project}/managedZones", s.wrap(principalFrom, s.listZones))
	mux.HandleFunc("POST /dns/v1/projects/{project}/managedZones", s.wrap(principalFrom, s.createZone))
	mux.HandleFunc("GET /dns/v1/projects/{project}/managedZones/{managedZone}", s.wrap(principalFrom, s.getZone))
	mux.HandleFunc("DELETE /dns/v1/projects/{project}/managedZones/{managedZone}", s.wrap(principalFrom, s.deleteZone))

	mux.HandleFunc("GET /dns/v1/projects/{project}/managedZones/{managedZone}/rrsets", s.wrap(principalFrom, s.listRrsets))
	mux.HandleFunc("POST /dns/v1/projects/{project}/managedZones/{managedZone}/rrsets", s.wrap(principalFrom, s.createRrset))
	mux.HandleFunc("GET /dns/v1/projects/{project}/managedZones/{managedZone}/rrsets/{name}/{type}", s.wrap(principalFrom, s.getRrset))
	mux.HandleFunc("DELETE /dns/v1/projects/{project}/managedZones/{managedZone}/rrsets/{name}/{type}", s.wrap(principalFrom, s.deleteRrset))
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

func writeAuthzErr(w http.ResponseWriter, err error) {
	if err == errDenied {
		gcperrors.PermissionDenied(w, "")
		return
	}
	gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
}

func splitAction(seg string) (name, action string) {
	if i := strings.IndexByte(seg, ':'); i >= 0 {
		return seg[:i], seg[i+1:]
	}
	return seg, ""
}

func (s *Service) createZone(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "dns.managedZones.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	zoneID, _ := body["name"].(string)
	zoneID = strings.TrimSpace(zoneID)
	if zoneID == "" {
		gcperrors.InvalidArgument(w, "name is required")
		return
	}
	dnsName, _ := body["dnsName"].(string)
	dnsName = strings.TrimSpace(dnsName)
	if dnsName == "" {
		gcperrors.InvalidArgument(w, "dnsName is required")
		return
	}
	if !strings.HasSuffix(dnsName, ".") {
		dnsName += "."
	}
	desc, _ := body["description"].(string)
	visibility, _ := body["visibility"].(string)
	if visibility == "" {
		visibility = "public"
	}
	switch visibility {
	case "public", "private":
	default:
		gcperrors.InvalidArgument(w, "visibility must be public or private")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	ns := []string{
		"ns-cloud-a1.noctaxris-gcp.lab.",
		"ns-cloud-a2.noctaxris-gcp.lab.",
	}
	zoneName := store.DNSZoneResourceName(project, zoneID)
	numericID := strconv.FormatInt(time.Now().UnixNano(), 10)
	created, err := s.Store.CreateDNSManagedZone(store.DNSManagedZone{
		Name: zoneName, ProjectID: project, ZoneID: zoneID, NumericID: numericID,
		DNSName: dnsName, Description: desc, Visibility: visibility,
		NameServersJSON: store.MarshalStringSlice(ns), CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "managed zone already exists")
		return
	}

	// Lab: seed NS + SOA like Cloud DNS does on zone create.
	soa := fmt.Sprintf("ns-cloud-a1.noctaxris-gcp.lab. cloud-dns-hostmaster.noctaxris-gcp.lab. 1 21600 3600 259200 300")
	_, _ = s.Store.CreateDNSRrset(store.DNSRrset{
		ProjectID: project, ZoneName: zoneName, ZoneID: zoneID,
		RrsetName: dnsName, RrsetType: "NS", TTL: 21600,
		RrdatasJSON: store.MarshalStringSlice(ns),
	})
	_, _ = s.Store.CreateDNSRrset(store.DNSRrset{
		ProjectID: project, ZoneName: zoneName, ZoneID: zoneID,
		RrsetName: dnsName, RrsetType: "SOA", TTL: 21600,
		RrdatasJSON: store.MarshalStringSlice([]string{soa}),
	})

	out, ok, err := s.Store.GetDNSManagedZone(zoneName)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created zone missing")
		return
	}
	writeJSON(w, http.StatusOK, toZoneJSON(out))
}

func (s *Service) getZone(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	zoneID, action := splitAction(r.PathValue("managedZone"))
	if action != "" {
		gcperrors.NotFound(w, "unknown Cloud DNS method")
		return
	}
	if err := s.require(p, "dns.managedZones.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	z, ok, err := s.Store.GetDNSManagedZoneByProjectID(project, zoneID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "ManagedZone not found")
		return
	}
	writeJSON(w, http.StatusOK, toZoneJSON(z))
}

func (s *Service) listZones(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "dns.managedZones.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListDNSManagedZones(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, z := range list {
		items = append(items, toZoneJSON(z))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":          "dns#managedZonesListResponse",
		"managedZones":  items,
	})
}

func (s *Service) deleteZone(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	zoneID, _ := splitAction(r.PathValue("managedZone"))
	if err := s.require(p, "dns.managedZones.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	z, ok, err := s.Store.GetDNSManagedZoneByProjectID(project, zoneID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "ManagedZone not found")
		return
	}
	deleted, err := s.Store.DeleteDNSManagedZone(z.Name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !deleted {
		gcperrors.NotFound(w, "ManagedZone not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) resolveZone(project, zoneID string) (store.DNSManagedZone, bool, error) {
	return s.Store.GetDNSManagedZoneByProjectID(project, zoneID)
}

func (s *Service) createRrset(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	zoneID, _ := splitAction(r.PathValue("managedZone"))
	if err := s.require(p, "dns.resourceRecordSets.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	z, ok, err := s.resolveZone(project, zoneID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "ManagedZone not found")
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	rrName, _ := body["name"].(string)
	rrName = strings.TrimSpace(rrName)
	rrType, _ := body["type"].(string)
	rrType = strings.TrimSpace(rrType)
	if rrName == "" || rrType == "" {
		gcperrors.InvalidArgument(w, "name and type are required")
		return
	}
	if !strings.HasSuffix(rrName, ".") {
		rrName += "."
	}
	ttl := int64(300)
	switch v := body["ttl"].(type) {
	case float64:
		ttl = int64(v)
	case json.Number:
		n, _ := v.Int64()
		ttl = n
	case string:
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			ttl = n
		}
	}
	rrdatas := extractStringSlice(body["rrdatas"])
	created, err := s.Store.CreateDNSRrset(store.DNSRrset{
		ProjectID: project, ZoneName: z.Name, ZoneID: z.ZoneID,
		RrsetName: rrName, RrsetType: rrType, TTL: ttl,
		RrdatasJSON: store.MarshalStringSlice(rrdatas),
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "rrset already exists")
		return
	}
	out, _, _ := s.Store.GetDNSRrset(z.Name, rrName, rrType)
	writeJSON(w, http.StatusOK, toRrsetJSON(out))
}

func (s *Service) getRrset(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	zoneID, _ := splitAction(r.PathValue("managedZone"))
	if err := s.require(p, "dns.resourceRecordSets.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	z, ok, err := s.resolveZone(project, zoneID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "ManagedZone not found")
		return
	}
	rrName := decodePathName(r.PathValue("name"))
	rrType, action := splitAction(r.PathValue("type"))
	if action != "" {
		gcperrors.NotFound(w, "unknown Cloud DNS method")
		return
	}
	out, found, err := s.Store.GetDNSRrset(z.Name, rrName, rrType)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !found {
		gcperrors.NotFound(w, "ResourceRecordSet not found")
		return
	}
	writeJSON(w, http.StatusOK, toRrsetJSON(out))
}

func (s *Service) listRrsets(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	zoneID, _ := splitAction(r.PathValue("managedZone"))
	if err := s.require(p, "dns.resourceRecordSets.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	z, ok, err := s.resolveZone(project, zoneID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "ManagedZone not found")
		return
	}
	list, err := s.Store.ListDNSRrsets(z.Name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	filterName := r.URL.Query().Get("name")
	filterType := strings.ToUpper(r.URL.Query().Get("type"))
	items := make([]map[string]any, 0, len(list))
	for _, rr := range list {
		if filterName != "" && rr.RrsetName != filterName {
			continue
		}
		if filterType != "" && rr.RrsetType != filterType {
			continue
		}
		items = append(items, toRrsetJSON(rr))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":   "dns#resourceRecordSetsListResponse",
		"rrsets": items,
	})
}

func (s *Service) deleteRrset(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	zoneID, _ := splitAction(r.PathValue("managedZone"))
	if err := s.require(p, "dns.resourceRecordSets.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	z, ok, err := s.resolveZone(project, zoneID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "ManagedZone not found")
		return
	}
	rrName := decodePathName(r.PathValue("name"))
	rrType, _ := splitAction(r.PathValue("type"))
	deleted, err := s.Store.DeleteDNSRrset(z.Name, rrName, rrType)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !deleted {
		gcperrors.NotFound(w, "ResourceRecordSet not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodePathName(raw string) string {
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		decoded = raw
	}
	return decoded
}

func extractStringSlice(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	default:
		return []string{}
	}
}

func toZoneJSON(z store.DNSManagedZone) map[string]any {
	return map[string]any{
		"kind":         "dns#managedZone",
		"name":         z.ZoneID,
		"id":           z.NumericID,
		"dnsName":      z.DNSName,
		"description":  z.Description,
		"visibility":   z.Visibility,
		"nameServers":  store.UnmarshalStringSlice(z.NameServersJSON),
		"creationTime": z.CreatedAt,
	}
}

func toRrsetJSON(r store.DNSRrset) map[string]any {
	return map[string]any{
		"kind":    "dns#resourceRecordSet",
		"name":    r.RrsetName,
		"type":    r.RrsetType,
		"ttl":     r.TTL,
		"rrdatas": store.UnmarshalStringSlice(r.RrdatasJSON),
	}
}
