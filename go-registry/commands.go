package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
)

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
	case "delete":
		fs := flag.NewFlagSet("node delete", flag.ContinueOnError)
		configPath := fs.String("config", defaultConfigPath, "config file path")
		id := fs.String("id", "", "node id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *id == "" {
			return errors.New("-id is required")
		}
		return runNodeDelete(*configPath, *id)
	default:
		return fmt.Errorf("unknown node subcommand %q", args[0])
	}
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

func runNodeDelete(configPath string, id string) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	nodes, err := loadNodes(cfg.DataFile)
	if err != nil {
		return err
	}
	next := nodes[:0]
	found := false
	for _, node := range nodes {
		if node.NodeID == id {
			found = true
			continue
		}
		next = append(next, node)
	}
	if !found {
		return errors.New("node not found")
	}
	return saveNodes(cfg.DataFile, next)
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
