package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/kshaikh/scada/internal/driver"
)

type TagConfig struct {
	ID          string            `yaml:"id" json:"id"`
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description" json:"description"`
	Type        string            `yaml:"type" json:"type"`
	Unit        string            `yaml:"unit" json:"unit"`
	Group       string            `yaml:"group" json:"group"`
	Access      string            `yaml:"access" json:"access"`
	Min         *float64          `yaml:"min" json:"min"`
	Max         *float64          `yaml:"max" json:"max"`
	Metadata    map[string]string `yaml:"metadata" json:"metadata"`
}

type AlarmConfig struct {
	ID        string  `yaml:"id" json:"id"`
	Name      string  `yaml:"name" json:"name"`
	TagID     string  `yaml:"tag_id" json:"tag_id"`
	Type      string  `yaml:"type" json:"type"`
	Threshold float64 `yaml:"threshold" json:"threshold"`
	Priority  int     `yaml:"priority" json:"priority"`
	Enabled   bool    `yaml:"enabled" json:"enabled"`
	Deadband  float64 `yaml:"deadband" json:"deadband"`
	Message   string  `yaml:"message" json:"message"`
}

type ModbusConfig struct {
	Name     string              `yaml:"name" json:"name"`
	Host     string              `yaml:"host" json:"host"`
	Port     int                 `yaml:"port" json:"port"`
	Unit     int                 `yaml:"unit" json:"unit"`
	Interval string              `yaml:"interval" json:"interval"`
	Maps     []driver.RegisterMap `yaml:"maps" json:"maps"`
}

type ServerConfig struct {
	HTTPAddr string `yaml:"http_addr" json:"http_addr"`
}

type HistoryConfig struct {
	Path string `yaml:"path" json:"path"`
}

type Config struct {
	Server   ServerConfig    `yaml:"server" json:"server"`
	History  HistoryConfig   `yaml:"history" json:"history"`
	Tags     []TagConfig     `yaml:"tags" json:"tags"`
	Alarms   []AlarmConfig   `yaml:"alarms" json:"alarms"`
	Modbus   []ModbusConfig  `yaml:"modbus" json:"modbus"`
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if c.Server.HTTPAddr == "" {
		c.Server.HTTPAddr = ":8080"
	}
	if c.History.Path == "" {
		c.History.Path = "scada.db"
	}
	return &c, nil
}

func (c *Config) Validate() error {
	if c.Server.HTTPAddr == "" {
		return fmt.Errorf("http_addr required")
	}
	return nil
}
