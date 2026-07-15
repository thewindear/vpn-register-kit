package main

import (
	"regexp"
	"sync"
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
