package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const configDirName = ".crunchyroll.config"
const configFileName = "config.json"

type appConfig struct {
	EtpRt        string `json:"etp_rt"`
	AudioLang    string `json:"audio_lang"`
	SubsLang     string `json:"subs_lang"`
	VideoQuality string `json:"video_quality"`
	AudioQuality string `json:"audio_quality"`
	OutputDir    string `json:"output_dir"`
}

func defaultAppConfig() appConfig {
	return appConfig{
		AudioLang:    "ja-JP",
		SubsLang:     "en-US",
		VideoQuality: "1080p",
		AudioQuality: "192k",
		OutputDir:    ".",
	}
}

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, configDirName), nil
}

func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

func legacyConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "crunchyroll-downloader", configFileName)
}

func loadConfigFile(path string) (appConfig, bool) {
	cfg := defaultAppConfig()
	if path == "" {
		return cfg, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, false
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultAppConfig(), false
	}
	return normalizeAppConfig(cfg), true
}

func normalizeAppConfig(cfg appConfig) appConfig {
	if cfg.AudioLang == "" {
		cfg.AudioLang = "ja-JP"
	}
	if cfg.SubsLang == "" {
		cfg.SubsLang = "en-US"
	}
	if cfg.VideoQuality == "" {
		cfg.VideoQuality = "1080p"
	}
	if cfg.AudioQuality == "" {
		cfg.AudioQuality = "192k"
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = "."
	}
	return cfg
}

func loadAppConfig() appConfig {
	path, err := configPath()
	if err != nil {
		return defaultAppConfig()
	}

	if cfg, ok := loadConfigFile(path); ok {
		return cfg
	}

	if legacy := legacyConfigPath(); legacy != "" {
		if cfg, ok := loadConfigFile(legacy); ok {
			saveAppConfig(cfg)
			return cfg
		}
	}

	saveAppConfig(defaultAppConfig())
	return defaultAppConfig()
}

func saveAppConfig(cfg appConfig) {
	path, err := configPath()
	if err != nil {
		return
	}
	data, err := json.MarshalIndent(normalizeAppConfig(cfg), "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0600)
}
