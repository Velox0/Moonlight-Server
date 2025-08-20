package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Config structure for server
type Config struct {
	Tokens          []string          `json:"tokens"`
	RetryCount      int               `json:"retry_count"`
	Port            int               `json:"port"`
	RegionHierarchy map[string]string `json:"region_hierarchy"`
}

func printConfigMinimal(cfg *Config) {
	// Build a stable, readable hierarchy string
	keys := make([]string, 0, len(cfg.RegionHierarchy))
	for k := range cfg.RegionHierarchy {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var pairs []string
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s->%s", k, cfg.RegionHierarchy[k]))
	}
	hierarchy := "—"
	if len(pairs) > 0 {
		hierarchy = strings.Join(pairs, ", ")
	}

	fmt.Printf(
		"================ CONFIG ================\n"+
			"Tokens           : [%s]\n"+
			"RetryCount       : %d\n"+
			"Region Hierarchy : %s\n"+
			"Port             : %d\n"+
			"=======================================\n",
		strings.Join(cfg.Tokens, ", "),
		cfg.RetryCount,
		hierarchy,
		cfg.Port,
	)
}

// LoadConfig loads server configuration
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	err = json.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}

	printConfigMinimal(&cfg)

	return &cfg, nil
}
