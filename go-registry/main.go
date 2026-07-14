package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultConfigPath = "./config.json"
	defaultListen     = ":8088"
	defaultDataFile   = "./nodes.json"
	maxRegisterBytes  = 64 * 1024
)

var (
	nodeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,63}$`)
	hostPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,252}[A-Za-z0-9]$`)
)

type Config struct {
	Listen          string   `json:"listen"`
	PublicBaseURL   string   `json:"public_base_url"`
	RegisterTokens  []string `json:"register_tokens"`
	SubscribeTokens []string `json:"subscribe_tokens"`
	DataFile        string   `json:"data_file"`
}

type Protocol struct {
	Type     string `json:"type"`
	Port     int    `json:"port"`
	Password string `json:"password,omitempty"`
	SNI      string `json:"sni,omitempty"`
	TLS      bool   `json:"tls,omitempty"`
	Method   string `json:"method,omitempty"`
}

type Node struct {
	SchemaVersion int        `json:"schema_version"`
	NodeID        string     `json:"node_id"`
	Name          string     `json:"name"`
	Region        string     `json:"region"`
	Host          string     `json:"host"`
	IP            string     `json:"ip,omitempty"`
	Protocols     []Protocol `json:"protocols"`
	UpdatedAt     string     `json:"updated_at"`
}

type nodeStoreFile struct {
	Nodes []Node `json:"nodes"`
}

type registryServer struct {
	cfg   Config
	mu    sync.Mutex
	store map[string]Node
}

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "server":
		err = commandServer(os.Args[2:])
	case "token":
		err = commandToken(os.Args[2:])
	case "node":
		err = commandNode(os.Args[2:])
	case "-h", "--help", "help":
		usage(os.Stdout)
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  vpn-registry server -config ./config.json")
	fmt.Fprintln(w, "  vpn-registry token list -config ./config.json")
	fmt.Fprintln(w, "  vpn-registry token create -config ./config.json")
	fmt.Fprintln(w, "  vpn-registry token delete -config ./config.json -token xxx")
	fmt.Fprintln(w, "  vpn-registry node list -config ./config.json")
	fmt.Fprintln(w, "  vpn-registry node show -config ./config.json -id us-la-001")
}

func commandServer(args []string) error {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	configPath := fs.String("config", defaultConfigPath, "config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, generated, err := ensureConfig(*configPath)
	if err != nil {
		return err
	}
	if generated {
		log.Printf("[INFO] config generated path=%s", *configPath)
		printGeneratedConfig(os.Stdout, cfg)
	}
	srv := newRegistryServer(cfg)
	log.Printf("[INFO] server started listen=%s data_file=%s", cfg.Listen, cfg.DataFile)
	return http.ListenAndServe(cfg.Listen, srv)
}

func commandToken(args []string) error {
	if len(args) < 1 {
		return errors.New("missing token subcommand")
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("token list", flag.ContinueOnError)
		configPath := fs.String("config", defaultConfigPath, "config file path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		cfg, err := loadConfig(*configPath)
		if err != nil {
			return err
		}
		return runTokenList(cfg, os.Stdout)
	case "create":
		fs := flag.NewFlagSet("token create", flag.ContinueOnError)
		configPath := fs.String("config", defaultConfigPath, "config file path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return runTokenCreate(*configPath, os.Stdout)
	case "delete":
		fs := flag.NewFlagSet("token delete", flag.ContinueOnError)
		configPath := fs.String("config", defaultConfigPath, "config file path")
		token := fs.String("token", "", "subscribe token to delete")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *token == "" {
			return errors.New("-token is required")
		}
		return runTokenDelete(*configPath, *token)
	default:
		return fmt.Errorf("unknown token subcommand %q", args[0])
	}
}

func commandNode(args []string) error {
	if len(args) < 1 {
		return errors.New("missing node subcommand")
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("node list", flag.ContinueOnError)
		configPath := fs.String("config", defaultConfigPath, "config file path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		cfg, err := loadConfig(*configPath)
		if err != nil {
			return err
		}
		nodes, err := loadNodes(cfg.DataFile)
		if err != nil {
			return err
		}
		return runNodeList(nodes, os.Stdout)
	case "show":
		fs := flag.NewFlagSet("node show", flag.ContinueOnError)
		configPath := fs.String("config", defaultConfigPath, "config file path")
		id := fs.String("id", "", "node id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *id == "" {
			return errors.New("-id is required")
		}
		cfg, err := loadConfig(*configPath)
		if err != nil {
			return err
		}
		nodes, err := loadNodes(cfg.DataFile)
		if err != nil {
			return err
		}
		return runNodeShow(nodes, *id, os.Stdout)
	default:
		return fmt.Errorf("unknown node subcommand %q", args[0])
	}
}

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

func newRegistryServer(cfg Config) http.Handler {
	nodes, err := loadNodes(cfg.DataFile)
	if err != nil {
		log.Printf("[WARN] load nodes failed path=%s error=%q", cfg.DataFile, err)
	}
	store := make(map[string]Node, len(nodes))
	for _, node := range nodes {
		store[node.NodeID] = node
	}
	s := &registryServer{cfg: cfg, store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/register", s.handleRegister)
	mux.HandleFunc("/sub", s.handleSub)
	mux.HandleFunc("/api/nodes", s.handleAPINodes)
	return mux
}

func (s *registryServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = io.WriteString(w, `{"status":"ok"}`+"\n")
}

func (s *registryServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	remote := remoteIP(r)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if !contains(s.cfg.RegisterTokens, token) {
		log.Printf("[WARN] register denied remote_ip=%s token=%s reason=invalid_token", remote, tokenHint(token))
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	defer r.Body.Close()
	var node Node
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRegisterBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&node); err != nil {
		log.Printf("[WARN] register invalid_json remote_ip=%s error=%q", remote, err)
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := validateNode(node); err != nil {
		log.Printf("[WARN] register invalid_node remote_ip=%s node=%s error=%q", remote, node.NodeID, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	node.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	s.mu.Lock()
	s.store[node.NodeID] = node
	nodes := s.sortedNodesLocked()
	err := saveNodes(s.cfg.DataFile, nodes)
	s.mu.Unlock()
	if err != nil {
		log.Printf("[ERROR] save nodes failed error=%q", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	log.Printf("[INFO] node registered remote_ip=%s node_id=%s name=%q host=%s protocols=%s", remote, node.NodeID, node.Name, node.Host, protocolNames(node.Protocols))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = io.WriteString(w, `{"status":"ok"}`+"\n")
}

func (s *registryServer) handleSub(w http.ResponseWriter, r *http.Request) {
	remote := remoteIP(r)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := r.URL.Query().Get("token")
	if !contains(s.cfg.SubscribeTokens, token) {
		log.Printf("[WARN] sub denied remote_ip=%s token=%s reason=invalid_token", remote, tokenHint(token))
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	format := strings.TrimSpace(r.URL.Query().Get("format"))
	if format == "" {
		format = "shadowrocket"
	}
	s.mu.Lock()
	nodes := s.sortedNodesLocked()
	s.mu.Unlock()
	body, contentType, err := renderSubscription(format, nodes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = io.WriteString(w, body)
	log.Printf("[INFO] sub request remote_ip=%s user_agent=%q token=%s format=%s status=200 nodes=%d", remote, r.UserAgent(), tokenHint(token), format, len(nodes))
}

func (s *registryServer) handleAPINodes(w http.ResponseWriter, r *http.Request) {
	remote := remoteIP(r)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if !contains(s.cfg.RegisterTokens, token) {
		log.Printf("[WARN] api nodes denied remote_ip=%s token=%s reason=invalid_token", remote, tokenHint(token))
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	nodes := s.sortedNodesLocked()
	s.mu.Unlock()
	type protocolSummary struct {
		Type string `json:"type"`
		Port int    `json:"port"`
	}
	type nodeSummary struct {
		NodeID    string            `json:"node_id"`
		Name      string            `json:"name"`
		Region    string            `json:"region"`
		Host      string            `json:"host"`
		IP        string            `json:"ip,omitempty"`
		Protocols []protocolSummary `json:"protocols"`
		UpdatedAt string            `json:"updated_at"`
	}
	out := struct {
		Nodes []nodeSummary `json:"nodes"`
	}{Nodes: make([]nodeSummary, 0, len(nodes))}
	for _, node := range nodes {
		item := nodeSummary{
			NodeID:    node.NodeID,
			Name:      node.Name,
			Region:    node.Region,
			Host:      node.Host,
			IP:        node.IP,
			UpdatedAt: node.UpdatedAt,
		}
		for _, p := range node.Protocols {
			item.Protocols = append(item.Protocols, protocolSummary{Type: p.Type, Port: p.Port})
		}
		out.Nodes = append(out.Nodes, item)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
	log.Printf("[INFO] api nodes request remote_ip=%s token=%s status=200 nodes=%d", remote, tokenHint(token), len(nodes))
}

func (s *registryServer) sortedNodesLocked() []Node {
	nodes := make([]Node, 0, len(s.store))
	for _, node := range s.store {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].NodeID < nodes[j].NodeID
	})
	return nodes
}

func validateNode(node Node) error {
	if node.SchemaVersion != 1 {
		return errors.New("schema_version must be 1")
	}
	if !nodeIDPattern.MatchString(node.NodeID) {
		return errors.New("invalid node_id")
	}
	if node.Name == "" || len(node.Name) > 80 {
		return errors.New("invalid name")
	}
	if !isValidHost(node.Host) {
		return errors.New("invalid host")
	}
	if len(node.Protocols) == 0 {
		return errors.New("at least one protocol is required")
	}
	for _, p := range node.Protocols {
		if p.Port < 1 || p.Port > 65535 {
			return fmt.Errorf("invalid port for %s", p.Type)
		}
		switch p.Type {
		case "trojan":
			if p.Password == "" {
				return errors.New("trojan password is required")
			}
			if p.SNI != "" && !isValidHost(p.SNI) {
				return errors.New("invalid trojan sni")
			}
		case "shadowsocks":
			if p.Method == "" || p.Password == "" {
				return errors.New("shadowsocks method and password are required")
			}
		default:
			return fmt.Errorf("unsupported protocol %q", p.Type)
		}
	}
	return nil
}

func isValidHost(host string) bool {
	if len(host) > 253 || host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return true
	}
	return hostPattern.MatchString(host) && strings.Contains(host, ".")
}

func loadNodes(path string) ([]Node, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var data nodeStoreFile
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, err
	}
	return data.Nodes, nil
}

func saveNodes(path string, nodes []Node) error {
	return writeJSONAtomic(path, nodeStoreFile{Nodes: nodes})
}

func renderSubscription(format string, nodes []Node) (string, string, error) {
	switch strings.ToLower(format) {
	case "", "shadowrocket", "links", "plain":
		return renderLinks(nodes), "text/plain; charset=utf-8", nil
	case "base64":
		links := renderLinks(nodes)
		return base64.StdEncoding.EncodeToString([]byte(links)), "text/plain; charset=utf-8", nil
	case "clash", "mihomo":
		return renderClash(nodes), "text/yaml; charset=utf-8", nil
	case "shadowrocket-conf", "shadowrocket-config", "sr-conf", "srconfig":
		return renderShadowrocketConfig(nodes), "text/plain; charset=utf-8", nil
	case "sing-box", "singbox":
		body, err := renderSingBox(nodes)
		return body, "application/json; charset=utf-8", err
	default:
		return "", "", fmt.Errorf("unsupported format %q", format)
	}
}

func renderLinks(nodes []Node) string {
	var links []string
	for _, node := range nodes {
		for _, p := range node.Protocols {
			switch p.Type {
			case "trojan":
				q := url.Values{}
				if p.SNI != "" {
					q.Set("sni", p.SNI)
					q.Set("peer", p.SNI)
				}
				if p.TLS {
					q.Set("security", "tls")
				}
				link := fmt.Sprintf("trojan://%s@%s:%d", url.QueryEscape(p.Password), node.Host, p.Port)
				if encoded := q.Encode(); encoded != "" {
					link += "?" + encoded
				}
				link += "#" + url.QueryEscape(node.Name+"-Trojan")
				links = append(links, link)
			case "shadowsocks":
				userInfo := base64.RawURLEncoding.EncodeToString([]byte(p.Method + ":" + p.Password))
				links = append(links, fmt.Sprintf("ss://%s@%s:%d#%s", userInfo, node.Host, p.Port, url.QueryEscape(node.Name+"-SS")))
			}
		}
	}
	return strings.Join(links, "\n") + "\n"
}

func renderClash(nodes []Node) string {
	var b strings.Builder
	names := make([]string, 0)
	b.WriteString("mixed-port: 7890\n")
	b.WriteString("allow-lan: false\n")
	b.WriteString("mode: rule\n")
	b.WriteString("log-level: info\n")
	b.WriteString("proxies:\n")
	for _, node := range nodes {
		for _, p := range node.Protocols {
			switch p.Type {
			case "trojan":
				name := node.Name + "-Trojan"
				names = append(names, name)
				fmt.Fprintf(&b, "  - name: %s\n", yamlQuote(name))
				fmt.Fprintf(&b, "    type: trojan\n")
				fmt.Fprintf(&b, "    server: %s\n", yamlQuote(node.Host))
				fmt.Fprintf(&b, "    port: %d\n", p.Port)
				fmt.Fprintf(&b, "    password: %s\n", yamlQuote(p.Password))
				fmt.Fprintf(&b, "    sni: %s\n", yamlQuote(firstNonEmpty(p.SNI, node.Host)))
				fmt.Fprintf(&b, "    skip-cert-verify: false\n")
				fmt.Fprintf(&b, "    udp: true\n")
			case "shadowsocks":
				name := node.Name + "-SS"
				names = append(names, name)
				fmt.Fprintf(&b, "  - name: %s\n", yamlQuote(name))
				fmt.Fprintf(&b, "    type: ss\n")
				fmt.Fprintf(&b, "    server: %s\n", yamlQuote(node.Host))
				fmt.Fprintf(&b, "    port: %d\n", p.Port)
				fmt.Fprintf(&b, "    cipher: %s\n", yamlQuote(p.Method))
				fmt.Fprintf(&b, "    password: %s\n", yamlQuote(p.Password))
				fmt.Fprintf(&b, "    udp: true\n")
			}
		}
	}
	b.WriteString("proxy-groups:\n")
	b.WriteString("  - name: PROXY\n")
	b.WriteString("    type: select\n")
	b.WriteString("    proxies:\n")
	for _, name := range names {
		fmt.Fprintf(&b, "      - %s\n", yamlQuote(name))
	}
	b.WriteString("      - DIRECT\n")
	b.WriteString("rule-providers:\n")
	b.WriteString("  direct:\n")
	b.WriteString("    type: http\n")
	b.WriteString("    behavior: domain\n")
	b.WriteString("    url: \"https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/direct.txt\"\n")
	b.WriteString("    path: ./ruleset/direct.yaml\n")
	b.WriteString("    interval: 86400\n")
	b.WriteString("  proxy:\n")
	b.WriteString("    type: http\n")
	b.WriteString("    behavior: domain\n")
	b.WriteString("    url: \"https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/proxy.txt\"\n")
	b.WriteString("    path: ./ruleset/proxy.yaml\n")
	b.WriteString("    interval: 86400\n")
	b.WriteString("  cncidr:\n")
	b.WriteString("    type: http\n")
	b.WriteString("    behavior: ipcidr\n")
	b.WriteString("    url: \"https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/cncidr.txt\"\n")
	b.WriteString("    path: ./ruleset/cncidr.yaml\n")
	b.WriteString("    interval: 86400\n")
	b.WriteString("  lancidr:\n")
	b.WriteString("    type: http\n")
	b.WriteString("    behavior: ipcidr\n")
	b.WriteString("    url: \"https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/lancidr.txt\"\n")
	b.WriteString("    path: ./ruleset/lancidr.yaml\n")
	b.WriteString("    interval: 86400\n")
	b.WriteString("rules:\n")
	b.WriteString("  - RULE-SET,lancidr,DIRECT\n")
	b.WriteString("  - RULE-SET,direct,DIRECT\n")
	b.WriteString("  - RULE-SET,cncidr,DIRECT\n")
	b.WriteString("  - RULE-SET,proxy,PROXY\n")
	b.WriteString("  - GEOIP,CN,DIRECT\n")
	b.WriteString("  - MATCH,PROXY\n")
	return b.String()
}

func renderShadowrocketConfig(nodes []Node) string {
	var (
		b     strings.Builder
		names []string
	)
	b.WriteString("[General]\n")
	b.WriteString("bypass-system = true\n")
	b.WriteString("skip-proxy = 127.0.0.1, 192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12, localhost, *.local\n")
	b.WriteString("\n")
	b.WriteString("[Proxy]\n")
	for _, node := range nodes {
		for _, p := range node.Protocols {
			switch p.Type {
			case "trojan":
				name := shadowrocketName(node.Name + "-Trojan")
				names = append(names, name)
				fmt.Fprintf(&b, "%s = trojan, %s, %d, password=%s, over-tls=true, tls-verification=true, sni=%s\n",
					name,
					shadowrocketValue(node.Host),
					p.Port,
					shadowrocketValue(p.Password),
					shadowrocketValue(firstNonEmpty(p.SNI, node.Host)),
				)
			case "shadowsocks":
				name := shadowrocketName(node.Name + "-SS")
				names = append(names, name)
				fmt.Fprintf(&b, "%s = ss, %s, %d, encrypt-method=%s, password=%s\n",
					name,
					shadowrocketValue(node.Host),
					p.Port,
					shadowrocketValue(p.Method),
					shadowrocketValue(p.Password),
				)
			}
		}
	}
	b.WriteString("\n")
	b.WriteString("[Proxy Group]\n")
	b.WriteString("PROXY = select")
	for _, name := range names {
		fmt.Fprintf(&b, ", %s", name)
	}
	b.WriteString(", DIRECT\n")
	b.WriteString("\n")
	b.WriteString("[Rule]\n")
	for _, ruleSetURL := range []string{
		"https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Shadowrocket/Lan/Lan.list",
		"https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Shadowrocket/China/China.list",
	} {
		fmt.Fprintf(&b, "RULE-SET,%s,DIRECT\n", ruleSetURL)
	}
	b.WriteString("GEOIP,CN,DIRECT\n")
	b.WriteString("FINAL,PROXY\n")
	return b.String()
}

func renderSingBox(nodes []Node) (string, error) {
	outbounds := []map[string]any{{"type": "direct", "tag": "direct"}}
	for _, node := range nodes {
		for _, p := range node.Protocols {
			switch p.Type {
			case "trojan":
				outbounds = append(outbounds, map[string]any{
					"type":        "trojan",
					"tag":         node.Name + "-Trojan",
					"server":      node.Host,
					"server_port": p.Port,
					"password":    p.Password,
					"tls": map[string]any{
						"enabled":     p.TLS,
						"server_name": firstNonEmpty(p.SNI, node.Host),
					},
				})
			case "shadowsocks":
				outbounds = append(outbounds, map[string]any{
					"type":        "shadowsocks",
					"tag":         node.Name + "-SS",
					"server":      node.Host,
					"server_port": p.Port,
					"method":      p.Method,
					"password":    p.Password,
				})
			}
		}
	}
	cfg := map[string]any{
		"log": map[string]any{"level": "info"},
		"dns": map[string]any{
			"servers": []map[string]any{{"tag": "google", "address": "8.8.8.8"}},
		},
		"outbounds": outbounds,
		"route": map[string]any{
			"final": "direct",
		},
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

func runTokenList(cfg Config, w io.Writer) error {
	fmt.Fprintln(w, "Register tokens:")
	for i, token := range cfg.RegisterTokens {
		fmt.Fprintf(w, "- reg_%d  %s\n", i+1, tokenHint(token))
	}
	fmt.Fprintln(w, "Subscribe tokens:")
	for i, token := range cfg.SubscribeTokens {
		fmt.Fprintf(w, "- sub_%d  %s\n", i+1, tokenHint(token))
	}
	return nil
}

func runTokenCreate(configPath string, w io.Writer) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	token, err := randomToken()
	if err != nil {
		return err
	}
	cfg.SubscribeTokens = append(cfg.SubscribeTokens, token)
	if err := saveConfigAtomic(configPath, cfg); err != nil {
		return err
	}
	fmt.Fprintln(w, "Created subscribe token:")
	fmt.Fprintf(w, "  %s\n", token)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Subscription URL:")
	fmt.Fprintf(w, "  %s/sub?token=%s\n", strings.TrimRight(cfg.PublicBaseURL, "/"), token)
	return nil
}

func runTokenDelete(configPath string, token string) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	next := cfg.SubscribeTokens[:0]
	found := false
	for _, item := range cfg.SubscribeTokens {
		if item == token {
			found = true
			continue
		}
		next = append(next, item)
	}
	if !found {
		return errors.New("subscribe token not found")
	}
	cfg.SubscribeTokens = next
	return saveConfigAtomic(configPath, cfg)
}

func runNodeList(nodes []Node, w io.Writer) error {
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].NodeID < nodes[j].NodeID })
	fmt.Fprintln(w, "Registered nodes:")
	for _, node := range nodes {
		fmt.Fprintf(w, "- %-16s %-16s %-6s %-32s %-22s updated=%s\n", node.NodeID, node.Name, node.Region, node.Host, protocolNames(node.Protocols), node.UpdatedAt)
	}
	return nil
}

func runNodeShow(nodes []Node, id string, w io.Writer) error {
	for _, node := range nodes {
		if node.NodeID != id {
			continue
		}
		fmt.Fprintf(w, "node_id: %s\n", node.NodeID)
		fmt.Fprintf(w, "name: %s\n", node.Name)
		fmt.Fprintf(w, "region: %s\n", node.Region)
		fmt.Fprintf(w, "host: %s\n", node.Host)
		if node.IP != "" {
			fmt.Fprintf(w, "ip: %s\n", node.IP)
		}
		fmt.Fprintln(w, "protocols:")
		for _, p := range node.Protocols {
			switch p.Type {
			case "trojan":
				fmt.Fprintf(w, "- trojan port=%d sni=%s password=%s\n", p.Port, firstNonEmpty(p.SNI, node.Host), tokenHint(p.Password))
			case "shadowsocks":
				fmt.Fprintf(w, "- shadowsocks port=%d method=%s password=%s\n", p.Port, p.Method, tokenHint(p.Password))
			}
		}
		fmt.Fprintf(w, "updated_at: %s\n", node.UpdatedAt)
		return nil
	}
	return errors.New("node not found")
}

func printGeneratedConfig(w io.Writer, cfg Config) {
	fmt.Fprintln(w, "Generated registry config")
	fmt.Fprintln(w, "Register token:")
	for _, token := range cfg.RegisterTokens {
		fmt.Fprintf(w, "  %s\n", token)
	}
	fmt.Fprintln(w, "Subscribe URLs:")
	for _, token := range cfg.SubscribeTokens {
		fmt.Fprintf(w, "  %s/sub?token=%s\n", strings.TrimRight(cfg.PublicBaseURL, "/"), token)
	}
}

func contains(items []string, value string) bool {
	if value == "" {
		return false
	}
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func tokenHint(token string) string {
	if token == "" {
		return "<empty>"
	}
	if len(token) <= 8 {
		return strings.Repeat("*", len(token))
	}
	return token[:4] + "..." + token[len(token)-4:]
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func protocolNames(protocols []Protocol) string {
	names := make([]string, 0, len(protocols))
	for _, p := range protocols {
		names = append(names, p.Type)
	}
	return strings.Join(names, ",")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func shadowrocketName(value string) string {
	value = strings.ReplaceAll(value, ",", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.TrimSpace(value)
}

func shadowrocketValue(value string) string {
	value = strings.ReplaceAll(value, ",", "%2C")
	value = strings.ReplaceAll(value, "\n", "")
	value = strings.ReplaceAll(value, "\r", "")
	return value
}

func yamlQuote(value string) string {
	escaped := strings.ReplaceAll(value, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

func atoiDefault(value string, fallback int) int {
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}
