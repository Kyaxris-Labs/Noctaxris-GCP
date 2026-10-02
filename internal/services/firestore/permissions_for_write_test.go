package firestore

import (
	"reflect"
	"testing"

	"cloud.google.com/go/firestore/apiv1/firestorepb"
)

func TestPermissionsForWriteCommitBatchWriteTable(t *testing.T) {
	doc := "projects/p/databases/(default)/documents/c/d"
	cases := []struct {
		name string
		w    *firestorepb.Write
		want []string
	}{
		{
			name: "delete",
			w:    &firestorepb.Write{Operation: &firestorepb.Write_Delete{Delete: doc}},
			want: []string{"datastore.entities.delete"},
		},
		{
			name: "update no precondition",
			w: &firestorepb.Write{Operation: &firestorepb.Write_Update{Update: &firestorepb.Document{Name: doc}}},
			want: []string{"datastore.entities.create", "datastore.entities.update"},
		},
		{
			name: "update exists false",
			w: &firestorepb.Write{
				Operation:       &firestorepb.Write_Update{Update: &firestorepb.Document{Name: doc}},
				CurrentDocument: &firestorepb.Precondition{ConditionType: &firestorepb.Precondition_Exists{Exists: false}},
			},
			want: []string{"datastore.entities.create"},
		},
		{
			name: "update exists true",
			w: &firestorepb.Write{
				Operation:       &firestorepb.Write_Update{Update: &firestorepb.Document{Name: doc}},
				CurrentDocument: &firestorepb.Precondition{ConditionType: &firestorepb.Precondition_Exists{Exists: true}},
			},
			want: []string{"datastore.entities.update"},
		},
		{
			name: "transform no precondition",
			w: &firestorepb.Write{
				Operation: &firestorepb.Write_Transform{Transform: &firestorepb.DocumentTransform{Document: doc}},
			},
			want: []string{"datastore.entities.create", "datastore.entities.update"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := permissionsForWrite(tc.w)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v want %#v", got, tc.want)
			}
		})
	}
	if got := permissionsForWrites(nil); !reflect.DeepEqual(got, []string{"datastore.databases.get"}) {
		t.Fatalf("empty writes: %#v", got)
	}
}
