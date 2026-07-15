package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

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
