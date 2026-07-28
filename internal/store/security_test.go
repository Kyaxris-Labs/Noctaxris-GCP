package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func openSecurityStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestCloudArmorSecurityPolicyCRUD(t *testing.T) {
	st := openSecurityStore(t)
	name := store.CloudArmorPolicyResourceName("p", "edge-deny")
	ok, err := st.CreateCloudArmorSecurityPolicy(store.CloudArmorSecurityPolicy{
		Name: name, ProjectID: "p", PolicyID: "edge-deny", Description: "lab",
	})
	if err != nil || !ok {
		t.Fatalf("create: ok=%v err=%v", ok, err)
	}
	ok, err = st.CreateCloudArmorSecurityPolicy(store.CloudArmorSecurityPolicy{
		Name: name, ProjectID: "p", PolicyID: "edge-deny",
	})
	if err != nil || ok {
		t.Fatalf("duplicate expected ok=false: ok=%v err=%v", ok, err)
	}
	pol, found, err := st.GetCloudArmorSecurityPolicy(name)
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if pol.PolicyID != "edge-deny" || pol.RulesJSON == "" {
		t.Fatalf("policy=%#v", pol)
	}
	list, err := st.ListCloudArmorSecurityPolicies("p")
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%d err=%v", len(list), err)
	}
	_, ok, err = st.UpdateCloudArmorSecurityPolicyRules(name, `[{"priority":1000,"action":"deny(403)"}]`, "updated")
	if err != nil || !ok {
		t.Fatalf("update rules: ok=%v err=%v", ok, err)
	}
	ok, err = st.DeleteCloudArmorSecurityPolicy(name)
	if err != nil || !ok {
		t.Fatalf("delete: ok=%v err=%v", ok, err)
	}
}

func TestCertManagerCertificateAndMapCRUD(t *testing.T) {
	st := openSecurityStore(t)
	certName := store.CertManagerCertificateResourceName("p", "global", "web-cert")
	ok, err := st.CreateCertManagerCertificate(store.CertManagerCertificate{
		Name: certName, ProjectID: "p", Location: "global", CertificateID: "web-cert",
		Description: "lab cert", Scope: "DEFAULT", CertType: "MANAGED",
	})
	if err != nil || !ok {
		t.Fatalf("create cert: ok=%v err=%v", ok, err)
	}
	c, found, err := st.GetCertManagerCertificate(certName)
	if err != nil || !found || c.CertificateID != "web-cert" {
		t.Fatalf("get cert: found=%v err=%v c=%#v", found, err, c)
	}
	certs, err := st.ListCertManagerCertificates("p", "global")
	if err != nil || len(certs) != 1 {
		t.Fatalf("list certs=%d err=%v", len(certs), err)
	}

	mapName := store.CertManagerCertificateMapResourceName("p", "global", "web-map")
	ok, err = st.CreateCertManagerCertificateMap(store.CertManagerCertificateMap{
		Name: mapName, ProjectID: "p", Location: "global", MapID: "web-map",
		Description: "lab map",
	})
	if err != nil || !ok {
		t.Fatalf("create map: ok=%v err=%v", ok, err)
	}
	maps, err := st.ListCertManagerCertificateMaps("p", "global")
	if err != nil || len(maps) != 1 {
		t.Fatalf("list maps=%d err=%v", len(maps), err)
	}
	ok, err = st.DeleteCertManagerCertificate(certName)
	if err != nil || !ok {
		t.Fatalf("delete cert: ok=%v err=%v", ok, err)
	}
	ok, err = st.DeleteCertManagerCertificateMap(mapName)
	if err != nil || !ok {
		t.Fatalf("delete map: ok=%v err=%v", ok, err)
	}
}

func TestCertManagerServiceUsageSeeded(t *testing.T) {
	st := openSecurityStore(t)
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	enabled, err := st.IsServiceEnabled("noctaxris-gcp-local", "certificatemanager.googleapis.com")
	if err != nil || !enabled {
		t.Fatalf("certificatemanager.googleapis.com seeded enabled=%v err=%v", enabled, err)
	}
}
