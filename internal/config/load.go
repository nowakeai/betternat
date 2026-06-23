package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

func LoadFile(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config %q: %w", path, err)
	}
	defer f.Close()
	return Load(f)
}

func Load(r io.Reader) (Config, error) {
	var cfg Config
	content, err := io.ReadAll(r)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 {
		return Config{}, fmt.Errorf("config is empty")
	}
	if trimmed[0] == '{' {
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.DisallowUnknownFields()
		err = dec.Decode(&cfg)
	} else {
		dec := yaml.NewDecoder(bytes.NewReader(trimmed))
		dec.KnownFields(true)
		err = dec.Decode(&cfg)
	}
	if err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (cfg *Config) Normalize() {
	if cfg.Local.NodeID == "" && cfg.Local.InstanceID != "" {
		cfg.Local.NodeID = cfg.Local.InstanceID
	}
	cfg.Local.InstanceID = ""
}

func (cfg Config) Validate() error {
	if cfg.Version == "" {
		return fmt.Errorf("config version is required")
	}
	if cfg.GatewayID == "" {
		return fmt.Errorf("gateway_id is required")
	}
	if cfg.Cloud == "" {
		return fmt.Errorf("cloud is required")
	}
	if cfg.Region == "" {
		return fmt.Errorf("region is required")
	}
	if cfg.Datapath.Engine == "" {
		return fmt.Errorf("datapath.engine is required")
	}
	if len(cfg.Datapath.PrivateCIDRs) == 0 {
		return fmt.Errorf("datapath.private_cidrs is required")
	}
	if cfg.Datapath.Engine == "loxilb" {
		if cfg.Datapath.LoxiLB.SNATTo == "" {
			return fmt.Errorf("datapath.loxilb.snat_to is required")
		}
		if cfg.Datapath.LoxiLB.SNATTo == "auto" && cfg.Datapath.LoxiLB.SNATInterface == "" {
			return fmt.Errorf("datapath.loxilb.snat_interface is required when snat_to is auto")
		}
	}
	if cfg.Observability.OutboundProbe.Enabled && cfg.Observability.OutboundProbe.URL == "" {
		return fmt.Errorf("observability.outbound_probe.url is required when outbound probe is enabled")
	}
	if cfg.Control.PeerAPI.Enabled && cfg.Control.PeerAPI.AuthToken == "" {
		return fmt.Errorf("control.peer_api.auth_token is required when peer API is enabled")
	}
	return nil
}
