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

func TestDeleteNodeRemovesItFromSubscriptions(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Listen:          ":0",
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
		"protocols": [
			{"type":"trojan","port":443,"password":"trojan-pass","sni":"hk01.example.com","tls":true}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer reg-token")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/nodes/hk01", nil)
	req.Header.Set("Authorization", "Bearer reg-token")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/sub?token=sub-token", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sub status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "hk01.example.com") {
		t.Fatalf("subscription still contains deleted node: %s", rec.Body.String())
	}

	nodes, err := loadNodes(cfg.DataFile)
	if err != nil {
		t.Fatalf("load nodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("stored node count = %d, want 0", len(nodes))
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
	for _, want := range []string{
		"rule-providers:",
		"cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/direct.txt",
		"cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/proxy.txt",
		"  - RULE-SET,lancidr,DIRECT",
		"  - RULE-SET,direct,DIRECT",
		"  - RULE-SET,cncidr,DIRECT",
		"  - RULE-SET,proxy,PROXY",
		"  - MATCH,PROXY",
	} {
		if !strings.Contains(clash, want) {
			t.Fatalf("clash output missing %q:\n%s", want, clash)
		}
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

func TestRenderShadowrocketConfig(t *testing.T) {
	nodes := []Node{{
		SchemaVersion: 1,
		NodeID:        "hk01",
		Name:          "HK-01",
		Region:        "HK",
		Host:          "hk01.example.com",
		Protocols: []Protocol{
			{Type: "trojan", Port: 443, Password: "trojan-pass", SNI: "hk01.example.com", TLS: true},
			{Type: "shadowsocks", Port: 8080, Method: "aes-128-gcm", Password: "ss-pass"},
		},
	}}

	conf, ctype, err := renderSubscription("shadowrocket-conf", nodes)
	if err != nil {
		t.Fatalf("render shadowrocket-conf: %v", err)
	}
	if ctype != "text/plain; charset=utf-8" {
		t.Fatalf("shadowrocket-conf content type = %q", ctype)
	}
	for _, want := range []string{
		"[Proxy]",
		"HK-01-Trojan = trojan, hk01.example.com, 443, password=trojan-pass, over-tls=true, tls-verification=true, sni=hk01.example.com",
		"HK-01-SS = ss, hk01.example.com, 8080, encrypt-method=aes-128-gcm, password=ss-pass",
		"[Proxy Group]",
		"PROXY = select, HK-01-Trojan, HK-01-SS, DIRECT",
		"[Rule]",
		"RULE-SET,https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Shadowrocket/Lan/Lan.list,DIRECT",
		"RULE-SET,https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Shadowrocket/China/China.list,DIRECT",
		"GEOIP,CN,DIRECT",
		"FINAL,PROXY",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("shadowrocket-conf output missing %q:\n%s", want, conf)
		}
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

func TestNodeDeleteCommand(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	dataPath := filepath.Join(dir, "nodes.json")
	cfg := Config{
		Listen:          ":8088",
		PublicBaseURL:   "https://sub.example.com",
		RegisterTokens:  []string{"reg-token"},
		SubscribeTokens: []string{"sub-token"},
		DataFile:        dataPath,
	}
	if err := saveConfigAtomic(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := saveNodes(dataPath, []Node{{
		SchemaVersion: 1,
		NodeID:        "hk01",
		Name:          "HK-01",
		Region:        "HK",
		Host:          "hk01.example.com",
		Protocols:     []Protocol{{Type: "trojan", Port: 443, Password: "tp", SNI: "hk01.example.com", TLS: true}},
	}}); err != nil {
		t.Fatalf("save nodes: %v", err)
	}

	if err := commandNode([]string{"delete", "-config", configPath, "-id", "hk01"}); err != nil {
		t.Fatalf("node delete: %v", err)
	}
	nodes, err := loadNodes(dataPath)
	if err != nil {
		t.Fatalf("load nodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("node count after delete = %d, want 0", len(nodes))
	}
}
