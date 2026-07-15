package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func ensureConfig(path string) (Config, bool, error) {
	cfg, err := loadConfig(path)
	if err == nil {
		return cfg, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Config{}, false, err
	}

	reg, err := randomToken()
	if err != nil {
		return Config{}, false, err
	}
	sub1, err := randomToken()
	if err != nil {
		return Config{}, false, err
	}
	sub2, err := randomToken()
	if err != nil {
		return Config{}, false, err
	}
	cfg = Config{
		Listen:          defaultListen,
		PublicBaseURL:   "http://127.0.0.1:8088",
		RegisterTokens:  []string{reg},
		SubscribeTokens: []string{sub1, sub2},
		DataFile:        defaultDataFile,
	}
	return cfg, true, saveConfigAtomic(path, cfg)
}

func loadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.Listen == "" {
		cfg.Listen = defaultListen
	}
	if cfg.DataFile == "" {
		cfg.DataFile = defaultDataFile
	}
	if cfg.PublicBaseURL == "" {
		cfg.PublicBaseURL = "http://127.0.0.1" + cfg.Listen
	}
	return cfg, nil
}

func saveConfigAtomic(path string, cfg Config) error {
	return writeJSONAtomic(path, cfg)
}

func writeJSONAtomic(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
