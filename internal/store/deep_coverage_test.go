package store_test

import (
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestStoreAnalyticsFirebaseMonitoringDeep(t *testing.T) {
	st := openTestStore(t)
	project := "p"

	d, created, err := st.CreateBQDataset(store.BQDataset{ProjectID: project, DatasetID: "analytics", FriendlyName: "A"})
	if err != nil || !created {
		t.Fatalf("dataset: %v %v", created, err)
	}
	list, err := st.ListBQDatasets(project)
	if err != nil || len(list) != 1 {
		t.Fatalf("list ds: %v", list)
	}
	got, ok, err := st.GetBQDataset(project, "analytics")
	if err != nil || !ok || got.DatasetID != d.DatasetID {
		t.Fatal(err)
	}
	tbl, created, err := st.CreateBQTable(store.BQTable{
		ProjectID: project, DatasetID: "analytics", TableID: "events",
		SchemaJSON: `[{"name":"n","type":"STRING"}]`,
	})
	if err != nil || !created {
		t.Fatalf("table: %v %v", created, err)
	}
	tables, err := st.ListBQTables(project, "analytics")
	if err != nil || len(tables) != 1 {
		t.Fatalf("list tables: %v", tables)
	}
	tg, ok, err := st.GetBQTable(project, "analytics", "events")
	if err != nil || !ok || tg.TableID != tbl.TableID {
		t.Fatal(err)
	}
	if err := st.InsertBQRows(project, "analytics", "events", []map[string]any{{"n": "a"}, {"n": "b"}}, []string{"1", "2"}); err != nil {
		t.Fatal(err)
	}
	n, err := st.CountBQRows(project, "analytics", "events")
	if err != nil || n != 2 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	page, err := st.ListBQRowsPage(project, "analytics", "events", 0, 1)
	if err != nil || len(page) != 1 {
		t.Fatalf("page=%v", page)
	}
	if err := st.PutBQJob(store.BQJob{ProjectID: project, JobID: "job1", State: "DONE", Query: "SELECT 1"}); err != nil {
		t.Fatal(err)
	}
	job, ok, err := st.GetBQJob(project, "job1")
	if err != nil || !ok || job.JobID != "job1" {
		t.Fatalf("job=%#v", job)
	}
	ok, err = st.DeleteBQTable(project, "analytics", "events")
	if err != nil || !ok {
		t.Fatal(err)
	}
	ok, err = st.DeleteBQDataset(project, "analytics")
	if err != nil || !ok {
		t.Fatal(err)
	}

	u, created, err := st.CreateFirebaseUser(store.FirebaseUser{
		ProjectID: project, Email: "u1@example.com", PasswordHash: "h1", DisplayName: "U1",
	})
	if err != nil || !created {
		t.Fatal(err)
	}
	byID, ok, err := st.GetFirebaseUserByLocalID(u.LocalID)
	if err != nil || !ok || byID.Email != u.Email {
		t.Fatal(err)
	}
	users, err := st.ListFirebaseUsers(project)
	if err != nil || len(users) != 1 {
		t.Fatalf("users=%v", users)
	}
	pageUsers, next, err := st.ListFirebaseUsersPage(project, 1, "")
	if err != nil || len(pageUsers) != 1 {
		t.Fatalf("page users=%v next=%q", pageUsers, next)
	}
	u.DisplayName = "U1b"
	if err := st.UpdateFirebaseUser(*u); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateFirebaseOOBCode(store.FirebaseOOBCode{
		OOBCode: "oob1", ProjectID: project, LocalID: u.LocalID, RequestType: "PASSWORD_RESET",
		Email: u.Email, ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	oob, ok, err := st.GetFirebaseOOBCode("oob1")
	if err != nil || !ok || oob.LocalID != u.LocalID {
		t.Fatal(err)
	}
	consumed, ok, err := st.ConsumeFirebaseOOBCode("oob1")
	if err != nil || !ok || consumed.OOBCode != "oob1" {
		t.Fatal(err)
	}
	ok, err = st.DeleteFirebaseUser(u.LocalID)
	if err != nil || !ok {
		t.Fatal(err)
	}

	md, created, err := st.CreateMetricDescriptor(store.MetricDescriptorRow{
		ProjectID: project, Type: "custom.googleapis.com/lab/deep", MetricKind: "GAUGE", ValueType: "DOUBLE",
	})
	if err != nil || !created {
		t.Fatal(err)
	}
	gmd, ok, err := st.GetMetricDescriptor(project, md.Type)
	if err != nil || !ok {
		t.Fatal(err)
	}
	mds, err := st.ListMetricDescriptors(project)
	if err != nil || len(mds) < 1 || gmd.Type == "" {
		t.Fatalf("mds=%v", mds)
	}
	if err := st.CreateTimeSeriesPoints([]store.TimeSeriesPoint{{
		ProjectID: project, MetricType: md.Type, ValueJSON: `{"doubleValue":9}`, EndTime: "2026-01-02T00:00:00Z",
	}}); err != nil {
		t.Fatal(err)
	}
	pts, err := st.ListTimeSeriesPoints(store.ListTimeSeriesFilter{ProjectID: project, MetricType: md.Type})
	if err != nil || len(pts) != 1 {
		t.Fatalf("pts=%v", pts)
	}
	pol, created, err := st.CreateAlertPolicy(store.AlertPolicyRow{
		ProjectID: project, PolicyID: "deep1", DisplayName: "d", Enabled: true, ConditionsJSON: `[]`,
	})
	if err != nil || !created {
		t.Fatal(err)
	}
	gp, ok, err := st.GetAlertPolicy(pol.Name)
	if err != nil || !ok {
		t.Fatal(err)
	}
	pols, err := st.ListAlertPolicies(project)
	if err != nil || len(pols) != 1 || gp.PolicyID == "" {
		t.Fatalf("pols=%v", pols)
	}
	pol.DisplayName = "d2"
	up, ok, err := st.UpdateAlertPolicy(*pol)
	if err != nil || !ok || up.DisplayName != "d2" {
		t.Fatalf("update pol %#v", up)
	}
	ok, err = st.DeleteAlertPolicy(pol.Name)
	if err != nil || !ok {
		t.Fatal(err)
	}
}

func TestStoreGCEUpdateAndGCSNotifications(t *testing.T) {
	st := openTestStore(t)
	instName := "projects/p/zones/us-central1-a/instances/vm2"
	ok, err := st.CreateGCEInstance(store.GCEInstance{
		Name: instName, ProjectID: "p", Zone: "us-central1-a", InstanceID: "vm2",
		MachineType: "zones/us-central1-a/machineTypes/e2-micro",
		NetworkInterfacesJSON: `[{"name":"nic0"}]`, BodyJSON: `{}`,
	})
	if err != nil || !ok {
		t.Fatal(err)
	}
	upd, ok, err := st.UpdateGCEInstanceBody(instName, "zones/us-central1-a/machineTypes/e2-small", `[{"name":"nic0"}]`, `{"labels":{"a":"b"}}`)
	if err != nil || !ok || upd.MachineType == "" {
		t.Fatalf("update inst %#v", upd)
	}
	netName := "projects/p/global/networks/vpc2"
	ok, err = st.CreateGCENetwork(store.GCENetwork{Name: netName, ProjectID: "p", NetworkID: "vpc2", BodyJSON: `{}`})
	if err != nil || !ok {
		t.Fatal(err)
	}
	gn, ok, err := st.GetGCENetwork(netName)
	if err != nil || !ok {
		t.Fatal(err)
	}
	nets, err := st.ListGCENetworks("p")
	if err != nil || len(nets) < 1 || gn.NetworkID == "" {
		t.Fatalf("nets=%v", nets)
	}
	un, ok, err := st.UpdateGCENetworkBody(netName, `{"autoCreateSubnetworks":true}`)
	if err != nil || !ok || un.BodyJSON == "" {
		t.Fatal(err)
	}
	subName := "projects/p/regions/us-central1/subnetworks/subnet2"
	ok, err = st.CreateGCESubnetwork(store.GCESubnetwork{
		Name: subName, ProjectID: "p", Region: "us-central1", SubnetworkID: "subnet2",
		Network: netName, IPCidrRange: "10.1.0.0/24", BodyJSON: `{}`,
	})
	if err != nil || !ok {
		t.Fatal(err)
	}
	gs, ok, err := st.GetGCESubnetwork(subName)
	if err != nil || !ok {
		t.Fatal(err)
	}
	subs, err := st.ListGCESubnetworks("p", "us-central1")
	if err != nil || len(subs) < 1 || gs.SubnetworkID == "" {
		t.Fatalf("subs=%v", subs)
	}
	us, ok, err := st.UpdateGCESubnetworkBody(subName, netName, "10.1.0.0/16", `{"privateIpGoogleAccess":true}`)
	if err != nil || !ok || us.IPCidrRange != "10.1.0.0/16" {
		t.Fatalf("update sub %#v", us)
	}
	fwName := "projects/p/global/firewalls/allow-http"
	ok, err = st.CreateGCEFirewall(store.GCEFirewall{
		Name: fwName, ProjectID: "p", FirewallID: "allow-http", Network: netName, BodyJSON: `{}`,
	})
	if err != nil || !ok {
		t.Fatal(err)
	}
	gf, ok, err := st.GetGCEFirewall(fwName)
	if err != nil || !ok {
		t.Fatal(err)
	}
	fws, err := st.ListGCEFirewalls("p")
	if err != nil || len(fws) < 1 || gf.FirewallID == "" {
		t.Fatalf("fws=%v", fws)
	}
	uf, ok, err := st.UpdateGCEFirewallBody(fwName, netName, `{"allowed":[{"IPProtocol":"tcp","ports":["80"]}]}`)
	if err != nil || !ok || uf.BodyJSON == "" {
		t.Fatal(err)
	}

	_, created, err := st.CreateBucket("notify-b", "p", "US", "STANDARD")
	if err != nil || !created {
		t.Fatal(err)
	}
	topic := "projects/p/topics/notify-t"
	if _, created, err := st.CreateTopic(topic, "p"); err != nil || !created {
		t.Fatal(err)
	}
	norm, err := store.NormalizePubSubNotificationTopic(topic)
	if err != nil || norm == "" {
		t.Fatal(err)
	}
	n, err := st.CreateNotificationConfig("notify-b", store.NotificationConfig{
		Topic: topic, PayloadFormat: "JSON_API_V1", EventTypes: []string{"OBJECT_FINALIZE"},
	})
	if err != nil {
		t.Fatal(err)
	}
	gotN, ok, err := st.GetNotificationConfig("notify-b", n.ID)
	if err != nil || !ok || gotN.ID != n.ID {
		t.Fatal(err)
	}
	listN, err := st.ListNotificationConfigs("notify-b")
	if err != nil || len(listN) != 1 {
		t.Fatalf("listN=%v", listN)
	}
	ok, err = st.DeleteNotificationConfig("notify-b", n.ID)
	if err != nil || !ok {
		t.Fatal(err)
	}
	_, _ = st.DeleteUploadSession("missing")
	objs, err := st.ListObjects("notify-b", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = objs
	ok, err = st.DeleteBucket("notify-b")
	if err != nil || !ok {
		t.Fatal(err)
	}
	_ = store.BucketIAMResource("notify-b")
}
