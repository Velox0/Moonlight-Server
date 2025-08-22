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
	GRPCPort        int               `json:"grpc_port,omitempty"`   // Optional: specify gRPC port explicitly
	EnableGRPC      bool              `json:"enable_grpc"`           // Enable/disable gRPC server
	GRPCConfig      *GRPCConfig       `json:"grpc_config,omitempty"` // gRPC specific settings
	RegionHierarchy map[string]string `json:"region_hierarchy"`
}

// GRPCConfig holds gRPC-specific configuration
type GRPCConfig struct {
	MaxMessageSize    int  `json:"max_message_size"`   // Max message size in bytes
	KeepaliveTime     int  `json:"keepalive_time"`     // Keepalive time in seconds
	KeepaliveTimeout  int  `json:"keepalive_timeout"`  // Keepalive timeout in seconds
	EnableReflection  bool `json:"enable_reflection"`  // Enable gRPC reflection for debugging
	EnableCompression bool `json:"enable_compression"` // Enable gRPC compression
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

	grpcStatus := "disabled"
	grpcPort := "—"
	if cfg.EnableGRPC {
		grpcStatus = "enabled"
		if cfg.GRPCPort > 0 {
			grpcPort = fmt.Sprintf("%d", cfg.GRPCPort)
		} else {
			grpcPort = fmt.Sprintf("%d (auto: HTTP+1)", cfg.Port+1)
		}
	}

	fmt.Printf(
		"================ CONFIG ================\n"+
			"Tokens           : [%s]\n"+
			"RetryCount       : %d\n"+
			"HTTP Port        : %d\n"+
			"gRPC Status      : %s\n"+
			"gRPC Port        : %s\n"+
			"Region Hierarchy : %s\n"+
			"=======================================\n",
		strings.Join(cfg.Tokens, ", "),
		cfg.RetryCount,
		cfg.Port,
		grpcStatus,
		grpcPort,
		hierarchy,
	)

	if cfg.EnableGRPC && cfg.GRPCConfig != nil {
		fmt.Printf("========== gRPC CONFIG ==========\n")
		fmt.Printf("Max Message Size : %d bytes\n", cfg.GRPCConfig.MaxMessageSize)
		fmt.Printf("Keepalive Time   : %d seconds\n", cfg.GRPCConfig.KeepaliveTime)
		fmt.Printf("Keepalive Timeout: %d seconds\n", cfg.GRPCConfig.KeepaliveTimeout)
		fmt.Printf("Reflection       : %t\n", cfg.GRPCConfig.EnableReflection)
		fmt.Printf("Compression      : %t\n", cfg.GRPCConfig.EnableCompression)
		fmt.Printf("===============================\n")
	}
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

	// Set defaults
	if cfg.Port == 0 {
		cfg.Port = 8080
	}

	// gRPC defaults
	if cfg.GRPCPort == 0 {
		cfg.GRPCPort = cfg.Port + 1 // Default to HTTP port + 1
	}

	if cfg.GRPCConfig == nil {
		cfg.GRPCConfig = &GRPCConfig{
			MaxMessageSize:    4 << 20, // 4MB
			KeepaliveTime:     60,      // 60 seconds
			KeepaliveTimeout:  10,      // 10 seconds
			EnableReflection:  false,   // Disabled by default for security
			EnableCompression: true,    // Enabled by default for mobile clients
		}
	}

	// Apply defaults to GRPCConfig if fields are zero
	if cfg.GRPCConfig.MaxMessageSize == 0 {
		cfg.GRPCConfig.MaxMessageSize = 4 << 20
	}
	if cfg.GRPCConfig.KeepaliveTime == 0 {
		cfg.GRPCConfig.KeepaliveTime = 60
	}
	if cfg.GRPCConfig.KeepaliveTimeout == 0 {
		cfg.GRPCConfig.KeepaliveTimeout = 10
	}

	printConfigMinimal(&cfg)

	return &cfg, nil
}
