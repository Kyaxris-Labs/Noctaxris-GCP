package store_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestGKEEdgeLBCDNRoundTrip(t *testing.T) {
	st := openTestStore(t)
	project := "noctaxris-gcp-local"
	loc := "us-central1"

	cname := "projects/" + project + "/locations/" + loc + "/clusters/lab"
	ok, err := st.CreateGKECluster(store.GKECluster{
		Name: cname, ProjectID: project, Location: loc, ClusterID: "lab", DisplayName: "Lab",
		Endpoint: "10.0.0.1",
	})
	if err != nil || !ok {
		t.Fatalf("create cluster: ok=%v err=%v", ok, err)
	}
	ok, err = st.CreateGKECluster(store.GKECluster{Name: cname, ProjectID: project, Location: loc, ClusterID: "lab"})
	if err != nil || ok {
		t.Fatalf("dup cluster: ok=%v err=%v", ok, err)
	}
	got, found, err := st.GetGKECluster(cname)
	if err != nil || !found || got.ClusterID != "lab" {
		t.Fatalf("get cluster: %#v found=%v err=%v", got, found, err)
	}
	list, err := st.ListGKEClusters(project, loc)
	if err != nil || len(list) != 1 {
		t.Fatalf("list clusters: %v err=%v", list, err)
	}
	if err := st.UpdateGKEClusterNestedDetail(cname, `{"engine":"ok"}`); err != nil {
		t.Fatal(err)
	}

	bsName := "projects/" + project + "/global/backendServices/bs1"
	ok, err = st.CreateLBBackendService(store.LBBackendService{
		Name: bsName, ProjectID: project, ServiceID: "bs1",
		BackendsJSON: `[{"gcsBucket":"origin-bucket","objectPrefix":"prefix/"}]`,
	})
	if err != nil || !ok {
		t.Fatalf("create bs: ok=%v err=%v", ok, err)
	}
	bs, found, err := st.GetLBBackendService(bsName)
	if err != nil || !found {
		t.Fatal(err)
	}
	bs2, found, err := st.GetLBBackendServiceByID(project, "global", "bs1")
	if err != nil || !found || bs2.Name != bs.Name {
		t.Fatalf("by id: %#v", bs2)
	}
	bss, err := st.ListLBBackendServices(project, "")
	if err != nil || len(bss) != 1 {
		t.Fatalf("list bs: %v", bss)
	}
	ok, err = st.UpdateLBBackendServiceSecurityPolicy(bsName, "projects/"+project+"/global/securityPolicies/sp")
	if err != nil || !ok {
		t.Fatalf("update sp: %v %v", ok, err)
	}
	bucket, prefix, parsed := store.ParseGCSOriginFromBackends(bs.BackendsJSON)
	if !parsed || bucket != "origin-bucket" {
		t.Fatalf("parse origin bucket=%q prefix=%q ok=%v", bucket, prefix, parsed)
	}
	id := store.NewLBResourceID()
	if id == "" {
		t.Fatal("NewLBResourceID empty")
	}

	umName := "projects/" + project + "/global/urlMaps/um1"
	ok, err = st.CreateLBURLMap(store.LBURLMap{
		Name: umName, ProjectID: project, MapID: "um1", DefaultService: bsName,
	})
	if err != nil || !ok {
		t.Fatalf("url map: %v %v", ok, err)
	}
	um, found, err := st.GetLBURLMap(umName)
	if err != nil || !found {
		t.Fatal(err)
	}
	_, found, err = st.GetLBURLMapByID(project, "global", "um1")
	if err != nil || !found {
		t.Fatal(err)
	}
	ums, err := st.ListLBURLMaps(project, "global")
	if err != nil || len(ums) != 1 || ums[0].Name != um.Name {
		t.Fatalf("list um: %v", ums)
	}

	proxyName := "projects/" + project + "/global/targetHttpsProxies/p1"
	ok, err = st.CreateLBTargetHTTPSProxy(store.LBTargetHTTPSProxy{
		Name: proxyName, ProjectID: project, ProxyID: "p1", URLMap: umName,
		SSLCertificatesJSON: `["cert1"]`,
	})
	if err != nil || !ok {
		t.Fatalf("proxy: %v %v", ok, err)
	}
	_, found, err = st.GetLBTargetHTTPSProxy(proxyName)
	if err != nil || !found {
		t.Fatal(err)
	}
	proxies, err := st.ListLBTargetHTTPSProxies(project, "global")
	if err != nil || len(proxies) != 1 {
		t.Fatalf("list proxies: %v", proxies)
	}
	ok, err = st.UpdateLBTargetHTTPSProxy(proxyName, "desc", umName, "sp")
	if err != nil || !ok {
		t.Fatalf("update proxy: %v %v", ok, err)
	}

	frName := "projects/" + project + "/global/forwardingRules/fr1"
	ok, err = st.CreateLBForwardingRule(store.LBForwardingRule{
		Name: frName, ProjectID: project, RuleID: "fr1", Target: proxyName,
	})
	if err != nil || !ok {
		t.Fatalf("fr: %v %v", ok, err)
	}
	_, found, err = st.GetLBForwardingRule(frName)
	if err != nil || !found {
		t.Fatal(err)
	}
	_, found, err = st.GetLBForwardingRuleByID(project, "global", "fr1")
	if err != nil || !found {
		t.Fatal(err)
	}
	frs, err := st.ListLBForwardingRules(project, "global")
	if err != nil || len(frs) != 1 {
		t.Fatalf("list fr: %v", frs)
	}

	cdnName := "projects/" + project + "/global/distributions/cdn1"
	ok, err = st.CreateCDNDistribution(store.CDNDistribution{
		Name: cdnName, ProjectID: project, DistributionID: "cdn1", Enabled: true,
		OriginJSON: `{"bucket":"origin-bucket"}`,
	})
	if err != nil || !ok {
		t.Fatalf("cdn: %v %v", ok, err)
	}
	_, found, err = st.GetCDNDistribution(cdnName)
	if err != nil || !found {
		t.Fatal(err)
	}
	_, found, err = st.GetCDNDistributionByID(project, "cdn1")
	if err != nil || !found {
		t.Fatal(err)
	}
	_, found, err = st.GetCDNDistributionByEdgeID("cdn1")
	if err != nil || !found {
		t.Fatal(err)
	}
	cdns, err := st.ListCDNDistributions(project)
	if err != nil || len(cdns) != 1 {
		t.Fatalf("list cdn: %v", cdns)
	}

	if ok, err := st.DeleteCDNDistribution(cdnName); err != nil || !ok {
		t.Fatalf("del cdn: %v %v", ok, err)
	}
	if ok, err := st.DeleteLBForwardingRule(frName); err != nil || !ok {
		t.Fatalf("del fr: %v %v", ok, err)
	}
	if ok, err := st.DeleteLBTargetHTTPSProxy(proxyName); err != nil || !ok {
		t.Fatalf("del proxy: %v %v", ok, err)
	}
	if ok, err := st.DeleteLBURLMap(umName); err != nil || !ok {
		t.Fatalf("del um: %v %v", ok, err)
	}
	if ok, err := st.DeleteLBBackendService(bsName); err != nil || !ok {
		t.Fatalf("del bs: %v %v", ok, err)
	}
	if ok, err := st.DeleteGKECluster(cname); err != nil || !ok {
		t.Fatalf("del cluster: %v %v", ok, err)
	}
}
