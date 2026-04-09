package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

const (
	DefaultPort     = 8080
	DefaultLogLevel = "info"
)

type Config struct {
	APIKey        string   `json:"api_key"`
	Token         string   `json:"token"`
	Cookies       string   `json:"cookies"`
	Tokens        []string `json:"tokens"`
	Proxy         string   `json:"proxy"`
	GeminiURL     string   `json:"gemini_url"`
	GeminiHomeURL string   `json:"gemini_home_url"`
	Port          int      `json:"port"`
	LogFile       string   `json:"log_file"`
	LogLevel      string   `json:"log_level"`
	Note          []string `json:"note"`
}

type Store struct {
	path string

	mu  sync.RWMutex
	cfg Config
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(s.path); os.IsNotExist(err) {
		defaultConfig := Config{
			Port:     DefaultPort,
			LogLevel: DefaultLogLevel,
			APIKey:   "your-api-key-here",
			Proxy:    "",
			Note:     []string{"Auto-generated config"},
		}

		data, err := json.MarshalIndent(defaultConfig, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal default config: %w", err)
		}
		if err := os.WriteFile(s.path, data, 0644); err != nil {
			return fmt.Errorf("failed to write default config: %w", err)
		}
		s.cfg = defaultConfig
		return nil
	}

	file, err := os.Open(s.path)
	if err != nil {
		return err
	}
	defer file.Close()

	var cfg Config
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		return err
	}

	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = DefaultLogLevel
	}

	s.cfg = cfg
	return nil
}

func (s *Store) Reload() error {
	return s.Load()
}

func (s *Store) Watch(onReload func() error) {
	go func() {
		var lastModTime time.Time
		for {
			time.Sleep(5 * time.Second)

			info, err := os.Stat(s.path)
			if err != nil {
				continue
			}

			modTime := info.ModTime()
			if !lastModTime.IsZero() && modTime.After(lastModTime) {
				if err := onReload(); err != nil {
					continue
				}
			}
			lastModTime = modTime
		}
	}()
}
