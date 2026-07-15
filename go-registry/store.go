package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
)

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
