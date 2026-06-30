package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type Config struct {
	NodeID                  string `json:"node_id"`
	Name                    string `json:"name"`
	Platform                string `json:"platform"`
	ClusterID               string `json:"cluster_id,omitempty"`
	PublicKey               string `json:"public_key"`
	SecretKey               string `json:"secret_key"`
	PoolUser                string `json:"pool_user,omitempty"`
	PoolPassHash            string `json:"pool_pass_hash,omitempty"`
	DataDir                 string `json:"-"`
	HTTPPort                int    `json:"http_port"`
	DiscoveryMode           string `json:"discovery_mode,omitempty"`
	TombstoneRetentionHours int    `json:"tombstone_retention_hours,omitempty"`
}

const defaultTombstoneRetentionHours = 7 * 24

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
	if cfg.DiscoveryMode == "" {
		cfg.DiscoveryMode = "auto"
	}
	if cfg.TombstoneRetentionHours <= 0 {
		cfg.TombstoneRetentionHours = defaultTombstoneRetentionHours
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
		NodeID:                  uuid.New().String(),
		Name:                    hostname,
		ClusterID:               "",
		DiscoveryMode:           "auto",
		TombstoneRetentionHours: defaultTombstoneRetentionHours,
		PublicKey:               base64.StdEncoding.EncodeToString(pub),
		SecretKey:               base64.StdEncoding.EncodeToString(priv),
		DataDir:                 dataDir,
		HTTPPort:                7788,
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

func (c *Config) SetPoolCredentials(user, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	c.PoolUser = user
	c.PoolPassHash = string(hash)
	return nil
}

func (c *Config) HasPoolCredentials() bool {
	return c.PoolUser != "" && c.PoolPassHash != ""
}

func (c *Config) TombstoneRetentionDuration() time.Duration {
	hours := c.TombstoneRetentionHours
	if hours <= 0 {
		hours = defaultTombstoneRetentionHours
	}
	return time.Duration(hours) * time.Hour
}

func (c *Config) SetTombstoneRetentionHours(hours int) {
	if hours <= 0 {
		hours = defaultTombstoneRetentionHours
	}
	c.TombstoneRetentionHours = hours
}

func (c *Config) CheckPoolPassword(password string) bool {
	if c.PoolUser == "" || c.PoolPassHash == "" {
		return false
	}
	// Legacy SHA256 hashes (pre-migration): accept and re-hash on success.
	if !strings.HasPrefix(c.PoolPassHash, "$2") {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(c.PoolPassHash), []byte(password)) == nil
}
