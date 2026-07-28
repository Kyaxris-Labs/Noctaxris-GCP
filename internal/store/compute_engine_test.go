package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestGCEStoreCRUD(t *testing.T) {
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

	instName := "projects/p/zones/us-central1-a/instances/vm1"
	ok, err := st.CreateGCEInstance(store.GCEInstance{
		Name: instName, ProjectID: "p", Zone: "us-central1-a", InstanceID: "vm1",
		MachineType: "zones/us-central1-a/machineTypes/e2-micro",
		NetworkInterfacesJSON: `[{"name":"nic0"}]`,
	})
	if err != nil || !ok {
		t.Fatalf("create instance: ok=%v err=%v", ok, err)
	}
	inst, found, err := st.GetGCEInstance(instName)
	if err != nil || !found || inst.Status != "RUNNING" {
		t.Fatalf("get instance: %#v found=%v err=%v", inst, found, err)
	}
	inst, found, err = st.SetGCEInstanceStatus(instName, "TERMINATED")
	if err != nil || !found || inst.Status != "TERMINATED" {
		t.Fatalf("stop theatre: %#v found=%v err=%v", inst, found, err)
	}
	list, err := st.ListGCEInstances("p", "us-central1-a")
	if err != nil || len(list) != 1 {
		t.Fatalf("list instances: %#v err=%v", list, err)
	}

	netName := "projects/p/global/networks/vpc1"
	ok, err = st.CreateGCENetwork(store.GCENetwork{
		Name: netName, ProjectID: "p", NetworkID: "vpc1",
		BodyJSON: `{"autoCreateSubnetworks":false}`,
	})
	if err != nil || !ok {
		t.Fatalf("create network: ok=%v err=%v", ok, err)
	}
	subName := "projects/p/regions/us-central1/subnetworks/subnet1"
	ok, err = st.CreateGCESubnetwork(store.GCESubnetwork{
		Name: subName, ProjectID: "p", Region: "us-central1", SubnetworkID: "subnet1",
		Network: netName, IPCidrRange: "10.0.0.0/24",
	})
	if err != nil || !ok {
		t.Fatalf("create subnet: ok=%v err=%v", ok, err)
	}
	fwName := "projects/p/global/firewalls/allow-ssh"
	ok, err = st.CreateGCEFirewall(store.GCEFirewall{
		Name: fwName, ProjectID: "p", FirewallID: "allow-ssh", Network: netName,
		BodyJSON: `{"allowed":[{"IPProtocol":"tcp","ports":["22"]}]}`,
	})
	if err != nil || !ok {
		t.Fatalf("create firewall: ok=%v err=%v", ok, err)
	}

	if ok, err := st.DeleteGCEInstance(instName); err != nil || !ok {
		t.Fatalf("delete instance: %v %v", ok, err)
	}
	if ok, err := st.DeleteGCESubnetwork(subName); err != nil || !ok {
		t.Fatalf("delete subnet: %v %v", ok, err)
	}
	if ok, err := st.DeleteGCEFirewall(fwName); err != nil || !ok {
		t.Fatalf("delete firewall: %v %v", ok, err)
	}
	if ok, err := st.DeleteGCENetwork(netName); err != nil || !ok {
		t.Fatalf("delete network: %v %v", ok, err)
	}
}

func TestComputeServiceUsageSeeded(t *testing.T) {
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
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	enabled, err := st.IsServiceEnabled("noctaxris-gcp-local", "compute.googleapis.com")
	if err != nil || !enabled {
		t.Fatalf("compute.googleapis.com seeded enabled=%v err=%v", enabled, err)
	}
}
