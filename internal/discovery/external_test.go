package discovery

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestApplyExternalEvent(t *testing.T) {
	d := New("self", "self", "test", 7788, "", "")
	if err := d.applyExternalEvent(externalEvent{
		Op:       "upsert",
		NodeID:   "peer-a",
		Name:     "Phone",
		Platform: "android",
		IP:       "192.168.1.20",
		Port:     7788,
	}); err != nil {
		t.Fatal(err)
	}

	nodes := d.Nodes()
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(nodes))
	}
	if got := nodes[0].Address; got != "192.168.1.20:7788" {
		t.Fatalf("address = %q, want 192.168.1.20:7788", got)
	}

	if err := d.applyExternalEvent(externalEvent{Op: "remove", NodeID: "peer-a"}); err != nil {
		t.Fatal(err)
	}
	if got := len(d.Nodes()); got != 0 {
		t.Fatalf("nodes after remove = %d, want 0", got)
	}
	select {
	case got := <-d.Removed():
		if got != "peer-a" {
			t.Fatalf("removed node = %q, want peer-a", got)
		}
	default:
		t.Fatal("expected removed node notification")
	}
}

func TestStartExternalContinuesAfterMalformedEvent(t *testing.T) {
	d := New("self", "self", "test", 7788, "", "")
	input := strings.NewReader("not-json\n" +
		`{"op":"upsert","node_id":"peer-b","name":"Mac","platform":"darwin","ip":"192.168.1.30","port":7788}` + "\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := d.StartExternal(ctx, input); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for len(d.Nodes()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := len(d.Nodes()); got != 1 {
		t.Fatalf("nodes = %d, want 1", got)
	}
}

func TestApplyExternalEventRejectsInvalidAddress(t *testing.T) {
	d := New("self", "self", "test", 7788, "", "")
	err := d.applyExternalEvent(externalEvent{
		Op:     "upsert",
		NodeID: "peer-c",
		IP:     "not-an-ip",
		Port:   7788,
	})
	if err == nil {
		t.Fatal("expected invalid IP error")
	}
}
