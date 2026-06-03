package discovery

import (
	"context"
	"testing"
	"time"
)

func TestDiscoveryStartStopDoesNotPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d := New("node-a", "node-a", "test", 0, "", "")
	if err := d.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	d.Stop()
	cancel()
	time.Sleep(100 * time.Millisecond)
}
