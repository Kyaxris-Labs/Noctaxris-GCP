package compute

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// Service serves Compute Engine API v1 REST (instances + VPC/firewall metadata theatre).
// No nested VMs, DinD, or qemu.
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Compute Engine v1 paths under /compute/v1/...
// Instance stop/start/reset are path actions parsed from a trailing wildcard segment
// (ServeMux cannot put ':' in patterns).
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	// Instances
	mux.HandleFunc("GET /compute/v1/projects/{project}/zones/{zone}/instances", s.wrap(principalFrom, s.listInstances))
	mux.HandleFunc("POST /compute/v1/projects/{project}/zones/{zone}/instances", s.wrap(principalFrom, s.insertInstance))
	mux.HandleFunc("GET /compute/v1/projects/{project}/zones/{zone}/instances/{instance}", s.wrap(principalFrom, s.getInstance))
	mux.HandleFunc("DELETE /compute/v1/projects/{project}/zones/{zone}/instances/{instance}", s.wrap(principalFrom, s.deleteInstance))
	mux.HandleFunc("PATCH /compute/v1/projects/{project}/zones/{zone}/instances/{instance}", s.wrap(principalFrom, s.patchInstance))
	mux.HandleFunc("POST /compute/v1/projects/{project}/zones/{zone}/instances/{rest...}", s.wrap(principalFrom, s.instancePOSTAction))

	// Global networks
	mux.HandleFunc("GET /compute/v1/projects/{project}/global/networks", s.wrap(principalFrom, s.listNetworks))
	mux.HandleFunc("POST /compute/v1/projects/{project}/global/networks", s.wrap(principalFrom, s.insertNetwork))
	mux.HandleFunc("GET /compute/v1/projects/{project}/global/networks/{network}", s.wrap(principalFrom, s.getNetwork))
	mux.HandleFunc("DELETE /compute/v1/projects/{project}/global/networks/{network}", s.wrap(principalFrom, s.deleteNetwork))
	mux.HandleFunc("PATCH /compute/v1/projects/{project}/global/networks/{network}", s.wrap(principalFrom, s.patchNetwork))

	// Regional subnetworks
	mux.HandleFunc("GET /compute/v1/projects/{project}/regions/{region}/subnetworks", s.wrap(principalFrom, s.listSubnetworks))
	mux.HandleFunc("POST /compute/v1/projects/{project}/regions/{region}/subnetworks", s.wrap(principalFrom, s.insertSubnetwork))
	mux.HandleFunc("GET /compute/v1/projects/{project}/regions/{region}/subnetworks/{subnetwork}", s.wrap(principalFrom, s.getSubnetwork))
	mux.HandleFunc("DELETE /compute/v1/projects/{project}/regions/{region}/subnetworks/{subnetwork}", s.wrap(principalFrom, s.deleteSubnetwork))
	mux.HandleFunc("PATCH /compute/v1/projects/{project}/regions/{region}/subnetworks/{subnetwork}", s.wrap(principalFrom, s.patchSubnetwork))

	// Global firewalls
	mux.HandleFunc("GET /compute/v1/projects/{project}/global/firewalls", s.wrap(principalFrom, s.listFirewalls))
	mux.HandleFunc("POST /compute/v1/projects/{project}/global/firewalls", s.wrap(principalFrom, s.insertFirewall))
	mux.HandleFunc("GET /compute/v1/projects/{project}/global/firewalls/{firewall}", s.wrap(principalFrom, s.getFirewallOrValidate))
	mux.HandleFunc("POST /compute/v1/projects/{project}/global/firewalls/{firewall}", s.wrap(principalFrom, s.getFirewallOrValidate))
	mux.HandleFunc("DELETE /compute/v1/projects/{project}/global/firewalls/{firewall}", s.wrap(principalFrom, s.deleteFirewall))
	mux.HandleFunc("PATCH /compute/v1/projects/{project}/global/firewalls/{firewall}", s.wrap(principalFrom, s.patchFirewall))
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

var errDenied = fmt.Errorf("permission denied")

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

func splitColonAction(seg string) (name, action string) {
	if i := strings.IndexByte(seg, ':'); i >= 0 {
		return seg[:i], seg[i+1:]
	}
	return seg, ""
}

func selfLink(parts ...string) string {
	return "https://www.googleapis.com/compute/v1/" + strings.Join(parts, "/")
}

func instanceResourceName(project, zone, id string) string {
	return fmt.Sprintf("projects/%s/zones/%s/instances/%s", project, zone, id)
}

func networkResourceName(project, id string) string {
	return fmt.Sprintf("projects/%s/global/networks/%s", project, id)
}

func subnetworkResourceName(project, region, id string) string {
	return fmt.Sprintf("projects/%s/regions/%s/subnetworks/%s", project, region, id)
}

func firewallResourceName(project, id string) string {
	return fmt.Sprintf("projects/%s/global/firewalls/%s", project, id)
}

func doneOperation(project, opType, targetLink, zone, region string) map[string]any {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := store.NewGCEResourceID()
	name := "operation-" + id
	op := map[string]any{
		"kind":          "compute#operation",
		"id":            id,
		"name":          name,
		"operationType": opType,
		"targetLink":    targetLink,
		"status":        "DONE",
		"progress":      100,
		"insertTime":    now,
		"startTime":     now,
		"endTime":       now,
		"selfLink":      selfLink("projects", project, "global", "operations", name),
	}
	if zone != "" {
		op["zone"] = selfLink("projects", project, "zones", zone)
		op["selfLink"] = selfLink("projects", project, "zones", zone, "operations", name)
	}
	if region != "" {
		op["region"] = selfLink("projects", project, "regions", region)
		op["selfLink"] = selfLink("projects", project, "regions", region, "operations", name)
	}
	return op
}

func decodeBody(r *http.Request) (map[string]any, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func marshalBodyExtras(body map[string]any, skip ...string) string {
	skipSet := map[string]struct{}{}
	for _, k := range skip {
		skipSet[k] = struct{}{}
	}
	extras := map[string]any{}
	for k, v := range body {
		if _, ok := skipSet[k]; ok {
			continue
		}
		extras[k] = v
	}
	raw, err := json.Marshal(extras)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func mergeJSON(baseJSON string, overlay map[string]any) string {
	base := map[string]any{}
	_ = json.Unmarshal([]byte(baseJSON), &base)
	if base == nil {
		base = map[string]any{}
	}
	for k, v := range overlay {
		base[k] = v
	}
	raw, err := json.Marshal(base)
	if err != nil {
		return baseJSON
	}
	return string(raw)
}

// --- Instances ---

func (s *Service) insertInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	zone := r.PathValue("zone")
	if err := s.require(p, "compute.instances.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	id, _ := body["name"].(string)
	if id == "" {
		gcperrors.InvalidArgument(w, "name is required")
		return
	}
	machineType, _ := body["machineType"].(string)
	if machineType == "" {
		machineType = selfLink("projects", project, "zones", zone, "machineTypes", "e2-micro")
	}
	niJSON := "[]"
	if ni, ok := body["networkInterfaces"]; ok {
		raw, _ := json.Marshal(ni)
		niJSON = string(raw)
	} else {
		niJSON = `[{"kind":"compute#networkInterface","name":"nic0","network":"` +
			selfLink("projects", project, "global", "networks", "default") + `"}]`
	}
	if meta, ok := body["metadata"]; ok {
		body["metadata"] = normalizeInstanceMetadata(meta)
	}
	name := instanceResourceName(project, zone, id)
	extras := marshalBodyExtras(body, "name", "machineType", "networkInterfaces", "status", "zone", "selfLink", "kind", "id", "creationTimestamp")
	created, err := s.Store.CreateGCEInstance(store.GCEInstance{
		Name: name, ProjectID: project, Zone: zone, InstanceID: id,
		MachineType: machineType, Status: "RUNNING", NetworkInterfacesJSON: niJSON, BodyJSON: extras,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "instance already exists")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, "insert", selfLink("projects", project, "zones", zone, "instances", id), zone, ""))
}

func (s *Service) listInstances(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	zone := r.PathValue("zone")
	if err := s.require(p, "compute.instances.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListGCEInstances(project, zone)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, inst := range list {
		items = append(items, toInstanceJSON(inst))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":     "compute#instanceList",
		"id":       fmt.Sprintf("projects/%s/zones/%s/instances", project, zone),
		"items":    items,
		"selfLink": selfLink("projects", project, "zones", zone, "instances"),
	})
}

func (s *Service) getInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	zone := r.PathValue("zone")
	id := r.PathValue("instance")
	if err := s.require(p, "compute.instances.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	inst, ok, err := s.Store.GetGCEInstance(instanceResourceName(project, zone, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	writeJSON(w, http.StatusOK, toInstanceJSON(inst))
}

func (s *Service) deleteInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	zone := r.PathValue("zone")
	id := r.PathValue("instance")
	if err := s.require(p, "compute.instances.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	target := selfLink("projects", project, "zones", zone, "instances", id)
	ok, err := s.Store.DeleteGCEInstance(instanceResourceName(project, zone, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, "delete", target, zone, ""))
}

func (s *Service) patchInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	zone := r.PathValue("zone")
	id := r.PathValue("instance")
	if err := s.require(p, "compute.instances.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	name := instanceResourceName(project, zone, id)
	cur, ok, err := s.Store.GetGCEInstance(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	machineType, _ := body["machineType"].(string)
	niJSON := ""
	if ni, ok := body["networkInterfaces"]; ok {
		raw, _ := json.Marshal(ni)
		niJSON = string(raw)
	}
	if meta, ok := body["metadata"]; ok {
		body["metadata"] = normalizeInstanceMetadata(meta)
	}
	extras := mergeJSON(cur.BodyJSON, map[string]any{})
	for k, v := range body {
		switch k {
		case "name", "machineType", "networkInterfaces", "status", "zone", "selfLink", "kind", "id", "creationTimestamp":
			continue
		default:
			extras = mergeJSON(extras, map[string]any{k: v})
		}
	}
	inst, ok, err := s.Store.UpdateGCEInstanceBody(name, machineType, niJSON, extras)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	writeJSON(w, http.StatusOK, toInstanceJSON(inst))
}

func (s *Service) instancePOSTAction(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	zone := r.PathValue("zone")
	rest := strings.Trim(r.PathValue("rest"), "/")
	parts := strings.Split(rest, "/")
	var id, action string
	switch len(parts) {
	case 1:
		// ServeMux-safe colon action: .../instances/{instance:stop}
		id, action = splitColonAction(parts[0])
	case 2:
		// Path action: .../instances/{instance}/stop|start|reset
		id, action = parts[0], parts[1]
	default:
		gcperrors.NotFound(w, "unknown Compute Engine method")
		return
	}
	if id == "" || action == "" {
		gcperrors.NotFound(w, "unknown Compute Engine method")
		return
	}
	name := instanceResourceName(project, zone, id)
	target := selfLink("projects", project, "zones", zone, "instances", id)
	var (
		perm   string
		status string
		opType string
	)
	switch action {
	case "stop":
		perm, status, opType = "compute.instances.stop", "TERMINATED", "stop"
	case "start":
		perm, status, opType = "compute.instances.start", "RUNNING", "start"
	case "reset":
		perm, status, opType = "compute.instances.reset", "RUNNING", "reset"
	default:
		gcperrors.NotFound(w, "unknown Compute Engine method")
		return
	}
	if err := s.require(p, perm, project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if _, ok, err := s.Store.GetGCEInstance(name); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	if _, ok, err := s.Store.SetGCEInstanceStatus(name, status); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, opType, target, zone, ""))
}

func toInstanceJSON(inst store.GCEInstance) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal([]byte(inst.BodyJSON), &out)
	if out == nil {
		out = map[string]any{}
	}
	var nis any
	_ = json.Unmarshal([]byte(inst.NetworkInterfacesJSON), &nis)
	if nis == nil {
		nis = []any{}
	}
	out["kind"] = "compute#instance"
	out["id"] = store.NewGCEResourceID()
	out["name"] = inst.InstanceID
	out["machineType"] = inst.MachineType
	out["status"] = inst.Status
	out["zone"] = selfLink("projects", inst.ProjectID, "zones", inst.Zone)
	out["networkInterfaces"] = nis
	out["selfLink"] = selfLink("projects", inst.ProjectID, "zones", inst.Zone, "instances", inst.InstanceID)
	out["creationTimestamp"] = inst.CreatedAt
	return out
}

// --- Networks ---

func (s *Service) insertNetwork(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "compute.networks.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	id, _ := body["name"].(string)
	if id == "" {
		gcperrors.InvalidArgument(w, "name is required")
		return
	}
	name := networkResourceName(project, id)
	extras := marshalBodyExtras(body, "name", "selfLink", "kind", "id", "creationTimestamp")
	created, err := s.Store.CreateGCENetwork(store.GCENetwork{
		Name: name, ProjectID: project, NetworkID: id, BodyJSON: extras,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "network already exists")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, "insert", selfLink("projects", project, "global", "networks", id), "", ""))
}

func (s *Service) listNetworks(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "compute.networks.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListGCENetworks(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, net := range list {
		items = append(items, toNetworkJSON(net))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":     "compute#networkList",
		"id":       "projects/" + project + "/global/networks",
		"items":    items,
		"selfLink": selfLink("projects", project, "global", "networks"),
	})
}

func (s *Service) getNetwork(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	id := r.PathValue("network")
	if err := s.require(p, "compute.networks.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	net, ok, err := s.Store.GetGCENetwork(networkResourceName(project, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Network not found")
		return
	}
	writeJSON(w, http.StatusOK, toNetworkJSON(net))
}

func (s *Service) deleteNetwork(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	id := r.PathValue("network")
	if err := s.require(p, "compute.networks.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	target := selfLink("projects", project, "global", "networks", id)
	ok, err := s.Store.DeleteGCENetwork(networkResourceName(project, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Network not found")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, "delete", target, "", ""))
}

func (s *Service) patchNetwork(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	id := r.PathValue("network")
	if err := s.require(p, "compute.networks.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	name := networkResourceName(project, id)
	cur, ok, err := s.Store.GetGCENetwork(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Network not found")
		return
	}
	extras := mergeJSON(cur.BodyJSON, marshalBodyExtrasMap(body, "name", "selfLink", "kind", "id", "creationTimestamp"))
	net, ok, err := s.Store.UpdateGCENetworkBody(name, extras)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Network not found")
		return
	}
	writeJSON(w, http.StatusOK, toNetworkJSON(net))
}

func marshalBodyExtrasMap(body map[string]any, skip ...string) map[string]any {
	skipSet := map[string]struct{}{}
	for _, k := range skip {
		skipSet[k] = struct{}{}
	}
	extras := map[string]any{}
	for k, v := range body {
		if _, ok := skipSet[k]; ok {
			continue
		}
		extras[k] = v
	}
	return extras
}

func toNetworkJSON(net store.GCENetwork) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal([]byte(net.BodyJSON), &out)
	if out == nil {
		out = map[string]any{}
	}
	out["kind"] = "compute#network"
	out["id"] = store.NewGCEResourceID()
	out["name"] = net.NetworkID
	out["selfLink"] = selfLink("projects", net.ProjectID, "global", "networks", net.NetworkID)
	out["creationTimestamp"] = net.CreatedAt
	if _, ok := out["autoCreateSubnetworks"]; !ok {
		out["autoCreateSubnetworks"] = false
	}
	return out
}

// --- Subnetworks ---

func (s *Service) insertSubnetwork(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	region := r.PathValue("region")
	if err := s.require(p, "compute.subnetworks.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	id, _ := body["name"].(string)
	if id == "" {
		gcperrors.InvalidArgument(w, "name is required")
		return
	}
	network, _ := body["network"].(string)
	ipCidr, _ := body["ipCidrRange"].(string)
	name := subnetworkResourceName(project, region, id)
	extras := marshalBodyExtras(body, "name", "network", "ipCidrRange", "selfLink", "kind", "id", "creationTimestamp", "region")
	created, err := s.Store.CreateGCESubnetwork(store.GCESubnetwork{
		Name: name, ProjectID: project, Region: region, SubnetworkID: id,
		Network: network, IPCidrRange: ipCidr, BodyJSON: extras,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "subnetwork already exists")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, "insert", selfLink("projects", project, "regions", region, "subnetworks", id), "", region))
}

func (s *Service) listSubnetworks(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	region := r.PathValue("region")
	if err := s.require(p, "compute.subnetworks.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListGCESubnetworks(project, region)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, sub := range list {
		items = append(items, toSubnetworkJSON(sub))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":     "compute#subnetworkList",
		"id":       fmt.Sprintf("projects/%s/regions/%s/subnetworks", project, region),
		"items":    items,
		"selfLink": selfLink("projects", project, "regions", region, "subnetworks"),
	})
}

func (s *Service) getSubnetwork(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	region := r.PathValue("region")
	id := r.PathValue("subnetwork")
	if err := s.require(p, "compute.subnetworks.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	sub, ok, err := s.Store.GetGCESubnetwork(subnetworkResourceName(project, region, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Subnetwork not found")
		return
	}
	writeJSON(w, http.StatusOK, toSubnetworkJSON(sub))
}

func (s *Service) deleteSubnetwork(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	region := r.PathValue("region")
	id := r.PathValue("subnetwork")
	if err := s.require(p, "compute.subnetworks.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	target := selfLink("projects", project, "regions", region, "subnetworks", id)
	ok, err := s.Store.DeleteGCESubnetwork(subnetworkResourceName(project, region, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Subnetwork not found")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, "delete", target, "", region))
}

func (s *Service) patchSubnetwork(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	region := r.PathValue("region")
	id := r.PathValue("subnetwork")
	if err := s.require(p, "compute.subnetworks.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	name := subnetworkResourceName(project, region, id)
	cur, ok, err := s.Store.GetGCESubnetwork(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Subnetwork not found")
		return
	}
	network, _ := body["network"].(string)
	ipCidr, _ := body["ipCidrRange"].(string)
	extras := mergeJSON(cur.BodyJSON, marshalBodyExtrasMap(body, "name", "network", "ipCidrRange", "selfLink", "kind", "id", "creationTimestamp", "region"))
	sub, ok, err := s.Store.UpdateGCESubnetworkBody(name, network, ipCidr, extras)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Subnetwork not found")
		return
	}
	writeJSON(w, http.StatusOK, toSubnetworkJSON(sub))
}

func toSubnetworkJSON(sub store.GCESubnetwork) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal([]byte(sub.BodyJSON), &out)
	if out == nil {
		out = map[string]any{}
	}
	out["kind"] = "compute#subnetwork"
	out["id"] = store.NewGCEResourceID()
	out["name"] = sub.SubnetworkID
	out["network"] = sub.Network
	out["ipCidrRange"] = sub.IPCidrRange
	out["region"] = selfLink("projects", sub.ProjectID, "regions", sub.Region)
	out["selfLink"] = selfLink("projects", sub.ProjectID, "regions", sub.Region, "subnetworks", sub.SubnetworkID)
	out["creationTimestamp"] = sub.CreatedAt
	return out
}

// --- Firewalls ---

func (s *Service) insertFirewall(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "compute.firewalls.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	id, _ := body["name"].(string)
	if id == "" {
		gcperrors.InvalidArgument(w, "name is required")
		return
	}
	network, _ := body["network"].(string)
	name := firewallResourceName(project, id)
	extras := marshalBodyExtras(body, "name", "network", "selfLink", "kind", "id", "creationTimestamp")
	created, err := s.Store.CreateGCEFirewall(store.GCEFirewall{
		Name: name, ProjectID: project, FirewallID: id, Network: network, BodyJSON: extras,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "firewall already exists")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, "insert", selfLink("projects", project, "global", "firewalls", id), "", ""))
}

func (s *Service) listFirewalls(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "compute.firewalls.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListGCEFirewalls(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, fw := range list {
		items = append(items, toFirewallJSON(fw))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":     "compute#firewallList",
		"id":       "projects/" + project + "/global/firewalls",
		"items":    items,
		"selfLink": selfLink("projects", project, "global", "firewalls"),
	})
}

func (s *Service) getFirewallOrValidate(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	seg := r.PathValue("firewall")
	id, action := splitColonAction(seg)
	if action == "validate" {
		s.validateFirewall(w, r, p, project, id)
		return
	}
	if action == "testIamPermissions" {
		s.testFirewallIamPermissions(w, r, p, project, id)
		return
	}
	if r.Method != http.MethodGet {
		gcperrors.NotFound(w, "unknown Compute Engine method")
		return
	}
	if err := s.require(p, "compute.firewalls.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	fw, ok, err := s.Store.GetGCEFirewall(firewallResourceName(project, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Firewall not found")
		return
	}
	writeJSON(w, http.StatusOK, toFirewallJSON(fw))
}

func (s *Service) validateFirewall(w http.ResponseWriter, r *http.Request, p authn.Principal, project, id string) {
	if err := s.require(p, "compute.firewalls.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	fw, ok, err := s.Store.GetGCEFirewall(firewallResourceName(project, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Firewall not found")
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	result := evalFirewallLite(toFirewallJSON(fw), body)
	writeJSON(w, http.StatusOK, result)
}

func (s *Service) testFirewallIamPermissions(w http.ResponseWriter, r *http.Request, p authn.Principal, project, id string) {
	if err := s.require(p, "compute.firewalls.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if _, ok, err := s.Store.GetGCEFirewall(firewallResourceName(project, id)); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Firewall not found")
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	perms, _ := body["permissions"].([]any)
	out := make([]string, 0, len(perms))
	for _, pRaw := range perms {
		perm, _ := pRaw.(string)
		if perm == "" {
			continue
		}
		ok, err := s.Authz.Evaluate(p.Email, p.IsRoot, perm, "projects/"+project)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		if ok {
			out = append(out, perm)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": out})
}

func (s *Service) deleteFirewall(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	id := r.PathValue("firewall")
	if err := s.require(p, "compute.firewalls.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	target := selfLink("projects", project, "global", "firewalls", id)
	ok, err := s.Store.DeleteGCEFirewall(firewallResourceName(project, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Firewall not found")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, "delete", target, "", ""))
}

func (s *Service) patchFirewall(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	id := r.PathValue("firewall")
	if err := s.require(p, "compute.firewalls.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	name := firewallResourceName(project, id)
	cur, ok, err := s.Store.GetGCEFirewall(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Firewall not found")
		return
	}
	network, _ := body["network"].(string)
	extras := mergeJSON(cur.BodyJSON, marshalBodyExtrasMap(body, "name", "network", "selfLink", "kind", "id", "creationTimestamp"))
	fw, ok, err := s.Store.UpdateGCEFirewallBody(name, network, extras)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Firewall not found")
		return
	}
	writeJSON(w, http.StatusOK, toFirewallJSON(fw))
}

func toFirewallJSON(fw store.GCEFirewall) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal([]byte(fw.BodyJSON), &out)
	if out == nil {
		out = map[string]any{}
	}
	out["kind"] = "compute#firewall"
	out["id"] = store.NewGCEResourceID()
	out["name"] = fw.FirewallID
	out["network"] = fw.Network
	out["selfLink"] = selfLink("projects", fw.ProjectID, "global", "firewalls", fw.FirewallID)
	out["creationTimestamp"] = fw.CreatedAt
	return out
}

// normalizeInstanceMetadata accepts either a string map or Compute items[] form
// and stores the googleapis-shaped metadata object for get.
func normalizeInstanceMetadata(raw any) map[string]any {
	switch m := raw.(type) {
	case map[string]any:
		if items, ok := m["items"].([]any); ok {
			return map[string]any{"kind": "compute#metadata", "items": items}
		}
		items := make([]any, 0, len(m))
		for k, v := range m {
			if k == "kind" || k == "fingerprint" {
				continue
			}
			items = append(items, map[string]any{"key": k, "value": fmt.Sprint(v)})
		}
		return map[string]any{"kind": "compute#metadata", "items": items}
	default:
		return map[string]any{"kind": "compute#metadata", "items": []any{}}
	}
}

// evalFirewallLite evaluates a single firewall rule against a probe request.
// Body fields: sourceIp, protocol, port (number or string). Fail-closed on miss.
func evalFirewallLite(fw map[string]any, probe map[string]any) map[string]any {
	srcIP, _ := probe["sourceIp"].(string)
	proto, _ := probe["protocol"].(string)
	proto = strings.ToLower(strings.TrimSpace(proto))
	port := probePort(probe["port"])

	direction, _ := fw["direction"].(string)
	if direction == "" {
		direction = "INGRESS"
	}
	disabled, _ := fw["disabled"].(bool)
	if disabled {
		return map[string]any{
			"matched": false, "allowed": false, "action": "NONE",
			"reason": "firewall disabled", "direction": direction,
		}
	}

	if !sourceRangeMatches(fw, srcIP) {
		return map[string]any{
			"matched": false, "allowed": false, "action": "NONE",
			"reason": "sourceIp not in sourceRanges", "direction": direction,
		}
	}

	if denied := matchL4(fw["denied"], proto, port); denied {
		return map[string]any{
			"matched": true, "allowed": false, "action": "DENY",
			"reason": "matched denied", "direction": direction,
		}
	}
	if allowed := matchL4(fw["allowed"], proto, port); allowed {
		return map[string]any{
			"matched": true, "allowed": true, "action": "ALLOW",
			"reason": "matched allowed", "direction": direction,
		}
	}
	return map[string]any{
		"matched": false, "allowed": false, "action": "NONE",
		"reason": "protocol/port not matched", "direction": direction,
	}
}

func probePort(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		var p int
		_, _ = fmt.Sscanf(n, "%d", &p)
		return p
	default:
		return 0
	}
}

func sourceRangeMatches(fw map[string]any, srcIP string) bool {
	ranges, _ := fw["sourceRanges"].([]any)
	if len(ranges) == 0 {
		// GCP default when omitted is 0.0.0.0/0 for ingress allow rules.
		return srcIP != ""
	}
	if srcIP == "" {
		return false
	}
	for _, r := range ranges {
		cidr, _ := r.(string)
		if cidrMatches(cidr, srcIP) {
			return true
		}
	}
	return false
}

func cidrMatches(cidr, ip string) bool {
	cidr = strings.TrimSpace(cidr)
	ip = strings.TrimSpace(ip)
	if cidr == "" || ip == "" {
		return false
	}
	if cidr == "0.0.0.0/0" || cidr == "::/0" || cidr == "*" {
		return true
	}
	if !strings.Contains(cidr, "/") {
		return cidr == ip
	}
	_, network, err := parseCIDRLite(cidr)
	if err != nil {
		return false
	}
	return network.contains(ip)
}

type ipNetLite struct {
	ones int
	mask uint32
	net  uint32
}

func parseCIDRLite(cidr string) (string, ipNetLite, error) {
	parts := strings.Split(cidr, "/")
	if len(parts) != 2 {
		return "", ipNetLite{}, fmt.Errorf("bad cidr")
	}
	ip := parseIPv4(parts[0])
	if ip == 0 && parts[0] != "0.0.0.0" {
		return "", ipNetLite{}, fmt.Errorf("bad ip")
	}
	var ones int
	if _, err := fmt.Sscanf(parts[1], "%d", &ones); err != nil || ones < 0 || ones > 32 {
		return "", ipNetLite{}, fmt.Errorf("bad prefix")
	}
	var mask uint32
	if ones == 0 {
		mask = 0
	} else {
		mask = ^uint32(0) << (32 - ones)
	}
	return parts[0], ipNetLite{ones: ones, mask: mask, net: ip & mask}, nil
}

func (n ipNetLite) contains(ipStr string) bool {
	ip := parseIPv4(ipStr)
	return ip&n.mask == n.net
}

func parseIPv4(s string) uint32 {
	var a, b, c, d uint32
	n, err := fmt.Sscanf(s, "%d.%d.%d.%d", &a, &b, &c, &d)
	if err != nil || n != 4 || a > 255 || b > 255 || c > 255 || d > 255 {
		return 0
	}
	return a<<24 | b<<16 | c<<8 | d
}

func matchL4(rules any, proto string, port int) bool {
	list, _ := rules.([]any)
	if len(list) == 0 {
		return false
	}
	for _, item := range list {
		rm, _ := item.(map[string]any)
		if rm == nil {
			continue
		}
		ipProto, _ := rm["IPProtocol"].(string)
		ipProto = strings.ToLower(strings.TrimSpace(ipProto))
		if ipProto == "" {
			ipProto = "all"
		}
		if ipProto != "all" && proto != "" && ipProto != proto {
			continue
		}
		ports, _ := rm["ports"].([]any)
		if len(ports) == 0 {
			// No ports means all ports for this protocol.
			return true
		}
		if port == 0 {
			return true
		}
		for _, p := range ports {
			ps := fmt.Sprint(p)
			if portInSpec(ps, port) {
				return true
			}
		}
	}
	return false
}

func portInSpec(spec string, port int) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return false
	}
	if strings.Contains(spec, "-") {
		var lo, hi int
		n, _ := fmt.Sscanf(spec, "%d-%d", &lo, &hi)
		if n == 2 {
			return port >= lo && port <= hi
		}
	}
	var p int
	_, _ = fmt.Sscanf(spec, "%d", &p)
	return p == port
}
