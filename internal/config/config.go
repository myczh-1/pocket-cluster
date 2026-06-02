package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

type Config struct {
	NodeID    string `json:"node_id"`
	Name      string `json:"name"`
	Platform  string `json:"platform"`
	ClusterID string `json:"cluster_id,omitempty"`
	PublicKey string `json:"public_key"`
	SecretKey string `json:"secret_key"`
	DataDir   string `json:"-"`
	HTTPPort  int    `json:"http_port"`
}

func Load(dataDir string) (*Config, error) {
	cfgPath := filepath.Join(dataDir, "config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return createNew(dataDir)
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.DataDir = dataDir
	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = 7788
	}
	return &cfg, nil
}

func createNew(dataDir string) (*Config, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "pocketcluster-node"
	}
	cfg := &Config{
		NodeID:    uuid.New().String(),
		Name:      hostname,
		ClusterID: uuid.New().String(),
		PublicKey: base64.StdEncoding.EncodeToString(pub),
		SecretKey: base64.StdEncoding.EncodeToString(priv),
		DataDir:   dataDir,
		HTTPPort:  7788,
	}
	cfg.Platform = detectPlatform()
	if err := cfg.Save(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Save() error {
	cfgPath := filepath.Join(c.DataDir, "config.json")
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, data, 0o600)
}

func (c *Config) Ed25519PrivateKey() (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(c.SecretKey)
	if err != nil {
		return nil, err
	}
	return ed25519.PrivateKey(b), nil
}

func (c *Config) Ed25519PublicKey() (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(c.PublicKey)
	if err != nil {
		return nil, err
	}
	return ed25519.PublicKey(b), nil
}
