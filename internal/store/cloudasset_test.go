package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestListInventoryAssetsAndFeeds(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	const project = "noctaxris-gcp-local"
	if err := st.EnsureRoot(project, "root@"+project+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateBucket("inv-bucket", project, "US", "STANDARD"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateTopic("projects/"+project+"/topics/inv-topic", project); err != nil {
		t.Fatal(err)
	}

	assets, err := st.ListInventoryAssets(project)
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]bool{}
	for _, a := range assets {
		types[a.AssetType] = true
	}
	for _, want := range []string{
		"cloudresourcemanager.googleapis.com/Project",
		"storage.googleapis.com/Bucket",
		"pubsub.googleapis.com/Topic",
		"iam.googleapis.com/ServiceAccount",
	} {
		if !types[want] {
			t.Fatalf("missing asset type %s in %#v", want, assets)
		}
	}

	parent := "projects/" + project
	feed, ok, err := st.CreateCloudAssetFeed(store.CloudAssetFeed{
		Parent: parent, FeedID: "f1", AssetTypesJSON: `["storage.googleapis.com/Bucket"]`,
		PubsubTopic: "projects/" + project + "/topics/inv-topic",
	})
	if err != nil || !ok {
		t.Fatalf("create feed ok=%v err=%v", ok, err)
	}
	got, ok, err := st.GetCloudAssetFeed(feed.Name)
	if err != nil || !ok || got.FeedID != "f1" {
		t.Fatalf("get feed %#v ok=%v err=%v", got, ok, err)
	}
	list, err := st.ListCloudAssetFeeds(parent)
	if err != nil || len(list) != 1 {
		t.Fatalf("list feeds %#v err=%v", list, err)
	}
	if err := st.InsertCloudAssetHistory(store.CloudAssetHistoryRow{
		Parent: parent, AssetName: assets[0].Name, AssetType: assets[0].AssetType, ContentJSON: assets[0].DataJSON,
	}); err != nil {
		t.Fatal(err)
	}
	hist, err := st.ListCloudAssetHistory(parent, nil)
	if err != nil || len(hist) != 1 {
		t.Fatalf("history %#v err=%v", hist, err)
	}
	deleted, err := st.DeleteCloudAssetFeed(feed.Name)
	if err != nil || !deleted {
		t.Fatalf("delete feed deleted=%v err=%v", deleted, err)
	}
}
