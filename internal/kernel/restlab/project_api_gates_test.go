package restlab_test

import (
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestRequireProjectAPIGates(t *testing.T) {
	st := struct {
		suStore
		vpcStore
	}{
		suStore:  suStore{enabled: true},
		vpcStore: vpcStore{},
	}
	rec := httptest.NewRecorder()
	p := authn.Principal{Email: "sa@p.iam.gserviceaccount.com", IsRoot: false}
	if !restlab.RequireProjectAPIGates(rec, st, p, "p", "bigquery.googleapis.com") {
		t.Fatal("enabled + vpc off must allow")
	}
	st.suStore.enabled = false
	rec = httptest.NewRecorder()
	if restlab.RequireProjectAPIGates(rec, st, p, "p", "bigquery.googleapis.com") {
		t.Fatal("disabled API must stop")
	}
	st.suStore.enabled = true
	st.vpcStore.deny = store.ErrVPCSCPerimeter
	t.Setenv("NOCTAXRIS_GCP_VPCSC_ENFORCE", "1")
	rec = httptest.NewRecorder()
	if restlab.RequireProjectAPIGates(rec, st, p, "p", "bigquery.googleapis.com") {
		t.Fatal("VPC-SC deny must stop when enforce on")
	}
}
