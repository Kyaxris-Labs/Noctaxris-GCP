package store_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestArTagsTheatreAndRepositoryDelete(t *testing.T) {
	st := openTestStore(t)
	const project = "noctaxris-gcp-local"
	repoName := "projects/" + project + "/locations/us-central1/repositories/lab"
	ok, err := st.CreateArRepository(store.ArRepository{
		Name: repoName, ProjectID: project, Location: "us-central1", RepositoryID: "lab",
	})
	if err != nil || !ok {
		t.Fatalf("repo ok=%v err=%v", ok, err)
	}
	pkgName := repoName + "/packages/app"
	ok, err = st.CreateArPackage(store.ArPackage{
		Name: pkgName, RepositoryName: repoName, PackageID: "app",
	})
	if err != nil || !ok {
		t.Fatalf("pkg ok=%v err=%v", ok, err)
	}
	verName := pkgName + "/versions/sha256:abc"
	ok, err = st.CreateArVersion(store.ArVersion{
		Name: verName, PackageName: pkgName, VersionID: "sha256:abc",
		RelatedTagsJSON: `["latest",{"name":"projects/x/locations/us/repositories/r/packages/p/tags/stable","version":"custom-ver"},{"name":""},1]`,
	})
	if err != nil || !ok {
		t.Fatalf("version ok=%v err=%v", ok, err)
	}

	tags, err := st.ListArTagsTheatreDeepen(pkgName)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) < 2 {
		t.Fatalf("tags=%#v", tags)
	}
	foundLatest, foundStable := false, false
	for _, tag := range tags {
		if tag.Name == pkgName+"/tags/latest" && tag.Version == verName {
			foundLatest = true
		}
		if tag.Name == pkgName+"/tags/stable" && tag.Version == "custom-ver" {
			foundStable = true
		}
	}
	if !foundLatest || !foundStable {
		t.Fatalf("expected latest+stable tags, got %#v", tags)
	}

	empty, err := st.ListArTagsTheatreDeepen(pkgName + "/missing")
	if err != nil || len(empty) != 0 {
		t.Fatalf("missing package tags=%#v err=%v", empty, err)
	}

	deleted, err := st.DeleteArRepository(repoName)
	if err != nil || !deleted {
		t.Fatalf("delete repo deleted=%v err=%v", deleted, err)
	}
	deleted, err = st.DeleteArRepository(repoName)
	if err != nil || deleted {
		t.Fatalf("second delete deleted=%v err=%v", deleted, err)
	}
}
