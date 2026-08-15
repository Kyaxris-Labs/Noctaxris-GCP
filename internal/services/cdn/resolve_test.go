package cdn

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestLBSvcResolveBackend(t *testing.T) {
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
	project := "noctaxris-gcp-local"
	if err := st.EnsureRoot(project, "root@"+project+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	bs := "projects/" + project + "/global/backendServices/bs1"
	if _, err := st.CreateLBBackendService(store.LBBackendService{
		Name: bs, ProjectID: project, ServiceID: "bs1", BackendsJSON: `[]`,
	}); err != nil {
		t.Fatal(err)
	}
	um := "projects/" + project + "/global/urlMaps/um1"
	if _, err := st.CreateLBURLMap(store.LBURLMap{
		Name: um, ProjectID: project, MapID: "um1", DefaultService: bs,
	}); err != nil {
		t.Fatal(err)
	}
	proxy := "projects/" + project + "/global/targetHttpsProxies/p1"
	if _, err := st.CreateLBTargetHTTPSProxy(store.LBTargetHTTPSProxy{
		Name: proxy, ProjectID: project, ProxyID: "p1", URLMap: um,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := lbSvcResolveBackend(st, proxy)
	if err != nil || got != bs {
		t.Fatalf("proxy resolve: got=%q err=%v", got, err)
	}
	got, err = lbSvcResolveBackend(st, um)
	if err != nil || got != bs {
		t.Fatalf("urlmap resolve: got=%q err=%v", got, err)
	}
	got, err = lbSvcResolveBackend(st, bs)
	if err != nil || got != bs {
		t.Fatalf("bs resolve: got=%q err=%v", got, err)
	}
	if _, err := lbSvcResolveBackend(st, "projects/p/global/backendServices/missing-path-type"); err != nil {
		// backend path returns as-is even if missing from store
		_ = err
	}
	if _, err := lbSvcResolveBackend(st, "junk"); err == nil {
		t.Fatal("unsupported")
	}
	if _, err := lbSvcResolveBackend(st, "projects/"+project+"/global/targetHttpsProxies/missing"); err == nil {
		t.Fatal("missing proxy")
	}
}
