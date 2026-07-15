package main

import (
	"net"
	"net/http"
	"strconv"
	"strings"
)

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
