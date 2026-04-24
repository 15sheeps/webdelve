package config

import (
	"github.com/goccy/go-yaml"
	"log"
	"os"
	"time"
)

type Config struct {
	Sandbox SandboxConfig `yaml:"sandbox"`
	Server  ServerConfig  `yaml:"server"`
	Builder BuilderConfig `yaml:"builder"`
	Log     LogConfig     `yaml:"log"`
}

type SandboxConfig struct {
	Image         string        `yaml:"image"`
	PoolLimit     int           `yaml:"pool_limit"` // max amount of containers (running + idle)
	StartTimeout  time.Duration `yaml:"start_timeout"`
	RemoveTimeout time.Duration `yaml:"remove_timeout"`
	HealthTimeout time.Duration `yaml:"health_timeout"`
	MemoryLimit   int64         `yaml:"memory_limit"`
}

type ServerConfig struct {
	MaxSourceSize int64         `yaml:"max_source_size"`
	Address       string        `yaml:"address"`
	RWTimeout     time.Duration `yaml:rw_timeout`
}

type BuilderConfig struct {
	ConcurrentLimit int64         `yaml:"concurrent_limit"` // max amount of build processes running at once
	BuildTimeout    time.Duration `yaml:"build_timeout"`
	MaxBinarySize   int           `yaml:"max_binary_size"`
}

type LogConfig struct {
	Level   string `yaml:"level"`
	Handler string `yaml:"handler"`
}

func MustLoad() (cfg Config) {
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		log.Fatalln("CONFIG_PATH is not set")
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		log.Fatalln("failed to read config file %s", cfgPath)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalln("failed to unmarshal config file: %s", cfgPath)
	}

	return
}
