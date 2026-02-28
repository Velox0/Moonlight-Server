package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestConfigFile(t *testing.T, dir string, name string, cfg map[string]interface{}) string {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	return path
}

func TestLoadConfigAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfigFile(t, dir, "mls.json", map[string]interface{}{
		"tokens": []string{"tok-a"},
	})

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.Port != 8080 {
		t.Fatalf("expected default port 8080, got %d", cfg.Port)
	}
	if cfg.WS.Path != "/ws" {
		t.Fatalf("expected default ws path /ws, got %s", cfg.WS.Path)
	}
	if cfg.WS.MaxConnections != 1000 {
		t.Fatalf("expected default max_connections 1000, got %d", cfg.WS.MaxConnections)
	}
	if cfg.WS.ReadTimeout != 300 || cfg.WS.WriteTimeout != 30 {
		t.Fatalf("unexpected ws timeout defaults read=%d write=%d", cfg.WS.ReadTimeout, cfg.WS.WriteTimeout)
	}
	if cfg.WS.HeartbeatInterval != 30 || cfg.WS.ConnectionCheckInterval != 60 {
		t.Fatalf("unexpected ws interval defaults heartbeat=%d check=%d", cfg.WS.HeartbeatInterval, cfg.WS.ConnectionCheckInterval)
	}
	if cfg.HTML.StaticPath != "/static" || cfg.HTML.IndexPath != "/" || cfg.HTML.DashboardPath != "/dashboard" {
		t.Fatalf("unexpected html defaults: %+v", cfg.HTML)
	}
}

func TestReloadConfigUsesSearchPathsAndUpdatesActiveConfig(t *testing.T) {
	dir := t.TempDir()
	invalidPath := filepath.Join(dir, "missing.json")
	validPath := writeTestConfigFile(t, dir, "mls.json", map[string]interface{}{
		"tokens": []string{"reload-token"},
		"port":   9090,
		"ws": map[string]interface{}{
			"enabled": true,
			"path":    "/ws",
		},
		"region_hierarchy": map[string]string{"us-east-1": "usa"},
	})

	setConfigSearchPaths([]string{invalidPath, validPath})
	path, err := reloadConfig()
	if err != nil {
		t.Fatalf("reloadConfig returned error: %v", err)
	}
	if path != validPath {
		t.Fatalf("expected loaded path %s, got %s", validPath, path)
	}

	active := getConfig()
	if active == nil {
		t.Fatal("expected active config to be set")
	}
	if active.Port != 9090 {
		t.Fatalf("expected active config port 9090, got %d", active.Port)
	}
	if !validToken("reload-token") {
		t.Fatal("expected token from reloaded config to be valid")
	}
	if validToken("wrong-token") {
		t.Fatal("expected wrong token to be invalid")
	}
}

func TestAssignNodeIDLooksHexAndUnique(t *testing.T) {
	seen := make(map[string]struct{})

	for i := 0; i < 500; i++ {
		id := assignNodeID()
		if id == "" {
			t.Fatal("assignNodeID returned empty id")
		}
		if strings.Trim(id, "0123456789abcdef") != "" {
			// all chars were in the allowed hex set
		} else {
			// no-op
		}
		for _, ch := range id {
			if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
				t.Fatalf("assignNodeID returned non-hex char %q in id %s", ch, id)
			}
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate node id generated: %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestGetParentRegionAndRegionMatchUseReloadedHierarchy(t *testing.T) {
	setActiveConfig(&Config{
		RegionHierarchy: map[string]string{
			"us-east-1": "usa",
			"usa":       "global",
		},
	}, "test")

	if parent := getParentRegion("us-east-1"); parent != "usa" {
		t.Fatalf("expected parent usa, got %s", parent)
	}
	if parent := getParentRegion("unknown"); parent != "" {
		t.Fatalf("expected empty parent for unknown region, got %s", parent)
	}
	if !regionMatch("usa", "us-east-1") {
		t.Fatal("expected usa to match us-east-1 via hierarchy")
	}
	if !regionMatch("global", "anything") {
		t.Fatal("expected global target to match any region")
	}
	if regionMatch("europe", "us-east-1") {
		t.Fatal("did not expect europe to match us-east-1")
	}
}
