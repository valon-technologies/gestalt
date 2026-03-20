package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/valon-technologies/toolshed/internal/config"
)

func runConfigCheck(args []string) error {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	path := resolveConfigPath(*configPath)

	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("config invalid: %v", err)
	}

	log.Printf("config file: %s", path)
	log.Printf("server:")
	log.Printf("  port:       %d", cfg.Server.Port)
	log.Printf("  base_url:   %s", maskEmpty(cfg.Server.BaseURL))
	log.Printf("  dev_mode:   %t", cfg.Server.DevMode)
	log.Printf("  encryption: %s", maskSecret(cfg.Server.EncryptionKey))
	log.Printf("auth:")
	log.Printf("  provider:   %s", cfg.Auth.Provider)
	log.Printf("datastore:")
	log.Printf("  provider:   %s", cfg.Datastore.Provider)
	log.Printf("secrets:")
	log.Printf("  provider:   %s", cfg.Secrets.Provider)

	if len(cfg.Integrations) > 0 {
		log.Printf("integrations: %d", len(cfg.Integrations))
		for name := range cfg.Integrations {
			intg := cfg.Integrations[name]
			source := "provider-dir"
			if intg.OpenAPI != "" {
				source = "openapi"
			} else if intg.Provider != "" {
				source = "provider-file"
			}
			auth := "oauth"
			if intg.Auth.Type == "manual" || intg.ConnectionMode == "manual" {
				auth = "manual"
			}
			ops := "all"
			if len(intg.AllowedOperations) > 0 {
				ops = fmt.Sprintf("%d allowed", len(intg.AllowedOperations))
			}
			log.Printf("  %s: source=%s auth=%s ops=%s", name, source, auth, ops)
		}
	}

	if len(cfg.Runtimes) > 0 {
		log.Printf("runtimes: %d", len(cfg.Runtimes))
		for name := range cfg.Runtimes {
			rt := cfg.Runtimes[name]
			log.Printf("  %s: type=%s providers=%s", name, rt.Type, strings.Join(rt.Providers, ","))
		}
	}

	if len(cfg.Bindings) > 0 {
		log.Printf("bindings: %d", len(cfg.Bindings))
		for name := range cfg.Bindings {
			b := cfg.Bindings[name]
			log.Printf("  %s: type=%s", name, b.Type)
		}
	}

	log.Printf("config ok")
	return nil
}

func maskSecret(s string) string {
	if s == "" {
		return "(not set)"
	}
	return "***"
}

func maskEmpty(s string) string {
	if s == "" {
		return "(not set)"
	}
	return s
}
