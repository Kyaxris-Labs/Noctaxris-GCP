package compute

import "testing"

func TestRedpandaStartCmdDefaultsAndHost(t *testing.T) {
	got := RedpandaStartCmd("")
	if len(got) < 5 || got[0] != "redpanda" {
		t.Fatalf("cmd=%v", got)
	}
	found := false
	for _, a := range got {
		if a == "internal://noctaxris-gcp-kafka:9092" {
			found = true
		}
	}
	if !found {
		t.Fatalf("default advertise missing: %v", got)
	}
	got2 := RedpandaStartCmd("my-broker")
	found = false
	for _, a := range got2 {
		if a == "internal://my-broker:9092" {
			found = true
		}
	}
	if !found {
		t.Fatalf("host advertise missing: %v", got2)
	}
}

func TestStripDockerLogHeader(t *testing.T) {
	plain := []byte("hello")
	if stripDockerLogHeader(plain) != "hello" {
		t.Fatalf("plain=%q", stripDockerLogHeader(plain))
	}
	framed := []byte{1, 0, 0, 0, 0, 0, 0, 5, 'w', 'o', 'r', 'l', 'd'}
	if stripDockerLogHeader(framed) != "world" {
		t.Fatalf("framed=%q", stripDockerLogHeader(framed))
	}
	stderr := []byte{2, 0, 0, 0, 0, 0, 0, 3, 'e', 'r', 'r'}
	if stripDockerLogHeader(stderr) != "err" {
		t.Fatalf("stderr=%q", stripDockerLogHeader(stderr))
	}
}
