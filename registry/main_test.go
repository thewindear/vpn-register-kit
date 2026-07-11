package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureConfigCreatesTokensAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg, generated, err := ensureConfig(path)
	if err != nil {
		t.Fatalf("ensureConfig returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config")
	}
	if cfg.Listen != ":8088" {
		t.Fatalf("listen = %q, want :8088", cfg.Listen)
	}
	if len(cfg.RegisterTokens) != 1 {
		t.Fatalf("register token count = %d, want 1", len(cfg.RegisterTokens))
	}
	if len(cfg.SubscribeTokens) != 2 {
		t.Fatalf("subscribe token count = %d, want 2", len(cfg.SubscribeTokens))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("config mode = %v, want 0600", info.Mode().Perm())
	}

	cfg2, generated2, err := ensureConfig(path)
	if err != nil {
		t.Fatalf("second ensureConfig returned error: %v", err)
	}
	if generated2 {
		t.Fatalf("did not expect second call to regenerate config")
	}
	if cfg2.RegisterTokens[0] != cfg.RegisterTokens[0] {
		t.Fatalf("register token changed across reload")
	}
}

func TestRegisterAndSubscribeShadowrocket(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Listen:          ":0",
		PublicBaseURL:   "https://sub.example.com",
		RegisterTokens:  []string{"reg-token"},
		SubscribeTokens: []string{"sub-token"},
		DataFile:        filepath.Join(dir, "nodes.json"),
	}
	srv := newRegistryServer(cfg)

	body := `{
		"schema_version": 1,
		"node_id": "hk01",
		"name": "HK-01",
		"region": "HK",
		"host": "hk01.example.com",
		"ip": "203.0.113.10",
		"protocols": [
			{"type":"trojan","port":443,"password":"trojan-pass","sni":"hk01.example.com","tls":true},
			{"type":"shadowsocks","port":8080,"method":"aes-128-gcm","password":"ss-pass"}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer reg-token")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/sub?token=sub-token", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sub status = %d body=%s", rec.Code, rec.Body.String())
	}
	got := rec.Body.String()
	if !strings.Contains(got, "trojan://trojan-pass@hk01.example.com:443") {
		t.Fatalf("subscription missing trojan link: %s", got)
	}
	if !strings.Contains(got, "ss://") || !strings.Contains(got, "#HK-01-SS") {
		t.Fatalf("subscription missing ss link: %s", got)
	}
}

func TestSubscribeRejectsInvalidToken(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Listen:          ":0",
		RegisterTokens:  []string{"reg-token"},
		SubscribeTokens: []string{"sub-token"},
		DataFile:        filepath.Join(dir, "nodes.json"),
	}
	srv := newRegistryServer(cfg)

	req := httptest.NewRequest(http.MethodGet, "/sub?token=bad-token", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRenderClashAndSingBox(t *testing.T) {
	nodes := []Node{{
		SchemaVersion: 1,
		NodeID:        "sg01",
		Name:          "SG-01",
		Region:        "SG",
		Host:          "sg01.example.com",
		Protocols: []Protocol{
			{Type: "trojan", Port: 443, Password: "tp", SNI: "sg01.example.com", TLS: true},
			{Type: "shadowsocks", Port: 8080, Method: "aes-128-gcm", Password: "sp"},
		},
	}}

	clash, ctype, err := renderSubscription("clash", nodes)
	if err != nil {
		t.Fatalf("render clash: %v", err)
	}
	if ctype != "text/yaml; charset=utf-8" {
		t.Fatalf("clash content type = %q", ctype)
	}
	if !strings.Contains(clash, "proxies:") || !strings.Contains(clash, "proxy-groups:") || !strings.Contains(clash, "rules:") {
		t.Fatalf("invalid clash output: %s", clash)
	}

	sb, ctype, err := renderSubscription("sing-box", nodes)
	if err != nil {
		t.Fatalf("render sing-box: %v", err)
	}
	if ctype != "application/json; charset=utf-8" {
		t.Fatalf("sing-box content type = %q", ctype)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(sb), &parsed); err != nil {
		t.Fatalf("sing-box output is not json: %v\n%s", err, sb)
	}
}

func TestTokenCreateAndDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := Config{
		Listen:          ":8088",
		PublicBaseURL:   "https://sub.example.com",
		RegisterTokens:  []string{"reg-token"},
		SubscribeTokens: []string{"sub-token"},
		DataFile:        filepath.Join(dir, "nodes.json"),
	}
	if err := saveConfigAtomic(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	var out bytes.Buffer
	if err := runTokenCreate(path, &out); err != nil {
		t.Fatalf("token create: %v", err)
	}
	if !strings.Contains(out.String(), "https://sub.example.com/sub?token=") {
		t.Fatalf("create output missing subscription URL: %s", out.String())
	}
	loaded, err := loadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(loaded.SubscribeTokens) != 2 {
		t.Fatalf("subscribe token count = %d, want 2", len(loaded.SubscribeTokens))
	}

	if err := runTokenDelete(path, loaded.SubscribeTokens[1]); err != nil {
		t.Fatalf("token delete: %v", err)
	}
	loaded, err = loadConfig(path)
	if err != nil {
		t.Fatalf("load after delete: %v", err)
	}
	if len(loaded.SubscribeTokens) != 1 {
		t.Fatalf("subscribe token count after delete = %d, want 1", len(loaded.SubscribeTokens))
	}
}
