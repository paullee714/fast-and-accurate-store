package server

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"fas/pkg/persistence"
)

// LoadConfigFile parses a simple key=value file and applies to Config (overrides existing fields).
func LoadConfigFile(path string, cfg *Config) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid config line: %s", line)
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])
		switch key {
		case "host":
			cfg.Host = val
		case "port":
			if p, err := strconv.Atoi(val); err == nil {
				cfg.Port = p
			} else {
				return fmt.Errorf("invalid port: %s", val)
			}
		case "aof":
			cfg.AOFPath = val
		case "rdb":
			cfg.RDBPath = val
		case "fsync":
			switch strings.ToLower(val) {
			case "always":
				cfg.FsyncPolicy = persistence.FsyncAlways
			case "everysec":
				cfg.FsyncPolicy = persistence.FsyncEverySec
			case "no":
				cfg.FsyncPolicy = persistence.FsyncNo
			default:
				return fmt.Errorf("invalid fsync: %s", val)
			}
		case "auth":
			cfg.AuthEnabled = strings.ToLower(val) == "true"
		case "requirepass":
			cfg.Password = val
		case "maxmemory":
			if m, err := strconv.ParseInt(val, 10, 64); err == nil {
				cfg.MaxMemory = m
			} else {
				return fmt.Errorf("invalid maxmemory: %s", val)
			}
		case "maxclients":
			if m, err := strconv.Atoi(val); err == nil {
				cfg.MaxClients = m
			} else {
				return fmt.Errorf("invalid maxclients: %s", val)
			}
		case "maxmemory-policy":
			ep, err := ParseEvictionPolicy(val)
			if err != nil {
				return err
			}
			cfg.Eviction = ep
		case "tls-cert":
			cfg.TLSCertPath = val
		case "tls-key":
			cfg.TLSKeyPath = val
		case "metrics-port":
			if m, err := strconv.Atoi(val); err == nil {
				cfg.MetricsPort = m
			} else {
				return fmt.Errorf("invalid metrics-port: %s", val)
			}
		default:
			// ignore unknown keys
		}
	}
	return scanner.Err()
}
