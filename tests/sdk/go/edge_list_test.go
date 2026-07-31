package sdk_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

func TestListDNSManagedZonesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/dns/v1/projects/" + project + "/managedZones"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list managed zones status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode managed zones: %v body=%s", err, body)
	}
	if _, ok := parsed["managedZones"]; !ok {
		t.Fatalf("missing managedZones field body=%s", body)
	}
}

func TestListCloudArmorSecurityPoliciesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/compute/v1/projects/" + project + "/global/securityPolicies"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list securityPolicies status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode securityPolicies: %v body=%s", err, body)
	}
	if kind, _ := parsed["kind"].(string); kind != "compute#securityPolicyList" {
		t.Fatalf("kind=%v want compute#securityPolicyList body=%s", parsed["kind"], body)
	}
	if _, ok := parsed["items"]; !ok {
		t.Fatalf("missing items field body=%s", body)
	}
}

func TestListCertificateManagerCertificatesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/locations/global/certificates"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list certificates status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode certificates: %v body=%s", err, body)
	}
	if _, ok := parsed["certificates"]; !ok {
		t.Fatalf("missing certificates field body=%s", body)
	}
}

func TestListFilestoreInstancesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/file/v1/projects/" + project + "/locations/us-central1/instances"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list filestore instances status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode filestore instances: %v body=%s", err, body)
	}
	if _, ok := parsed["instances"]; !ok {
		t.Fatalf("missing instances field body=%s", body)
	}
}

func TestGKEClusterAndCDNEdgeSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()
	clusterID := uniqueID("sdk-gke")
	if len(clusterID) > 40 {
		clusterID = clusterID[:40]
	}
	base := ep + "/container/v1/projects/" + project + "/locations/us-central1/clusters"
	status, body := doJSON(t, http.MethodPost, base+"?clusterId="+url.QueryEscape(clusterID), token, map[string]any{
		"displayName": "SDK GKE",
	})
	if status != http.StatusOK {
		t.Fatalf("create cluster status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, base+"/"+clusterID, token, nil)
	})
	status, body = doJSON(t, http.MethodGet, base+"/"+clusterID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get cluster status=%d body=%s", status, body)
	}

	distID := uniqueID("sdk-cdn")
	if len(distID) > 40 {
		distID = distID[:40]
	}
	distBase := ep + "/v1/projects/" + project + "/global/distributions"
	status, body = doJSON(t, http.MethodPost, distBase+"?distributionId="+url.QueryEscape(distID), token, map[string]any{
		"origin": map[string]any{"gcs": map[string]any{"bucket": "missing-bucket-for-smoke"}},
	})
	if status != http.StatusOK {
		t.Fatalf("create distribution status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, distBase+"/"+distID, token, nil)
	})
	edgeURL := ep + "/cdn/" + distID + "/obj.txt"
	req, err := http.NewRequest(http.MethodGet, edgeURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("edge without object want 404, got %d", resp.StatusCode)
	}
}

func TestListGKEClustersSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/container/v1/projects/" + project + "/locations/us-central1/clusters"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list GKE clusters status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode GKE clusters: %v body=%s", err, body)
	}
	if _, ok := parsed["clusters"]; !ok {
		t.Fatalf("missing clusters field body=%s", body)
	}
}

func TestListLoadBalancingBackendServicesSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/compute/v1/projects/" + project + "/global/backendServices"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list backendServices status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode backendServices: %v body=%s", err, body)
	}
	if kind, _ := parsed["kind"].(string); kind != "compute#backendServiceList" {
		t.Fatalf("kind=%v want compute#backendServiceList body=%s", parsed["kind"], body)
	}
	if _, ok := parsed["items"]; !ok {
		t.Fatalf("missing items field body=%s", body)
	}
}

func TestListCDNDistributionsSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/global/distributions"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list CDN distributions status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode CDN distributions: %v body=%s", err, body)
	}
	if _, ok := parsed["distributions"]; !ok {
		t.Fatalf("missing distributions field body=%s", body)
	}
}
