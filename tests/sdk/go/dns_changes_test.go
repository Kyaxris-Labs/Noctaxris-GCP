package sdk_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// Live HTTP smoke for the Terraform google_dns_record_set path (Changes.create + rrset get).
// Unit coverage lives in internal/services/dns; this only checks the listener + auth wiring.
func TestDNSChangesRrsetSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	zoneID := uniqueID("sdk-dns")
	if len(zoneID) > 40 {
		zoneID = zoneID[len(zoneID)-40:]
	}
	dnsName := zoneID + ".noctaxris-gcp.lab."
	zoneBase := fmt.Sprintf("%s/dns/v1/projects/%s/managedZones", ep, project)

	status, body := doJSON(t, http.MethodPost, zoneBase, token, map[string]any{
		"name":        zoneID,
		"dnsName":     dnsName,
		"visibility":  "public",
		"description": "SDK Changes smoke",
	})
	if status != http.StatusOK {
		t.Fatalf("create zone status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, zoneBase+"/"+url.PathEscape(zoneID), token, nil)
	})

	rrName := "www." + dnsName
	changeURL := zoneBase + "/" + url.PathEscape(zoneID) + "/changes"
	status, body = doJSON(t, http.MethodPost, changeURL, token, map[string]any{
		"additions": []map[string]any{
			{"name": rrName, "type": "A", "ttl": 120, "rrdatas": []string{"203.0.113.10"}},
		},
		"deletions": []any{},
	})
	if status != http.StatusOK {
		t.Fatalf("changes.create status=%d body=%s", status, body)
	}
	var change map[string]any
	if err := json.Unmarshal(body, &change); err != nil {
		t.Fatalf("decode change: %v body=%s", err, body)
	}
	if change["status"] != "done" {
		t.Fatalf("change status=%v want done body=%s", change["status"], body)
	}
	changeID, _ := change["id"].(string)
	if changeID == "" {
		t.Fatalf("missing change id body=%s", body)
	}

	getChangeURL := changeURL + "/" + url.PathEscape(changeID)
	status, body = doJSON(t, http.MethodGet, getChangeURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("changes.get status=%d body=%s", status, body)
	}

	rrPath := fmt.Sprintf("%s/%s/rrsets/%s/%s",
		zoneBase, url.PathEscape(zoneID), url.PathEscape(rrName), "A")
	status, body = doJSON(t, http.MethodGet, rrPath, token, nil)
	if status != http.StatusOK {
		t.Fatalf("rrset get status=%d body=%s", status, body)
	}
	var rr map[string]any
	if err := json.Unmarshal(body, &rr); err != nil {
		t.Fatalf("decode rrset: %v body=%s", err, body)
	}
	rrdatas, _ := rr["rrdatas"].([]any)
	if len(rrdatas) != 1 || rrdatas[0] != "203.0.113.10" {
		t.Fatalf("rrdatas=%v want [203.0.113.10] body=%s", rrdatas, body)
	}
}
