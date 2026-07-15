package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

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
