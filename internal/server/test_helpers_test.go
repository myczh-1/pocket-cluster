package server

import (
	"testing"

	"github.com/pocketcluster/agent/internal/config"
)

func newTestConfig(t *testing.T, nodeID string) *config.Config {
	t.Helper()
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.NodeID = nodeID
	cfg.ClusterID = "cluster"
	cfg.Name = nodeID
	cfg.Platform = "test"
	return cfg
}
