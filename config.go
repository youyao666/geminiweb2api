package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
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

var config Config
var configMutex sync.RWMutex

func getConfigSnapshot() Config {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return config
}

func loadConfig() error {
	configMutex.Lock()
	defer configMutex.Unlock()

	const configFile = "config.json"
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		defaultConfig := Config{
			Port:     8080,
			LogLevel: LogLevelInfo,
			APIKey:   "your-api-key-here",
			Proxy:    "",
			Note:     []string{"Auto-generated config"},
		}
		data, err := json.MarshalIndent(defaultConfig, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal default config: %v", err)
		}
		if err := os.WriteFile(configFile, data, 0644); err != nil {
			return fmt.Errorf("failed to write default config: %v", err)
		}
		config = defaultConfig
		return nil
	}

	file, err := os.Open(configFile)
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return err
	}

	if config.Port == 0 {
		config.Port = 8080
	}
	if config.LogLevel == "" {
		config.LogLevel = LogLevelInfo
	}
	return nil
}

func reloadConfig() error {
	if err := loadConfig(); err != nil {
		return err
	}
	if err := initLogger(); err != nil {
		return err
	}
	initHTTPClient()
	logger.Info("配置文件已成功重载")
	return nil
}

func startConfigWatcher() {
	go func() {
		var lastModTime time.Time
		for {
			time.Sleep(5 * time.Second)
			info, err := os.Stat("config.json")
			if err != nil {
				continue
			}
			modTime := info.ModTime()
			if !lastModTime.IsZero() && modTime.After(lastModTime) {
				if err := reloadConfig(); err != nil {
					logger.Error("重载配置文件失败: %v", err)
				}
			}
			lastModTime = modTime
		}
	}()
}
