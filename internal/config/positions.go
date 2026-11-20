package config

import (
	"fmt"
	"net/url"
	"time"
)

type PositionConfig struct {
	CheckInterval time.Duration
	StaleAfter    time.Duration
	Workers       int
	QueueSize     int
	BatchSize     int
	PushURL       string
	PushTimeout   time.Duration
}

func loadPositions() (PositionConfig, error) {
	var cfg PositionConfig
	for _, setting := range []struct {
		name string
		out  *time.Duration
	}{
		{"POSITION_CHECK_INTERVAL", &cfg.CheckInterval},
		{"POSITION_STALE_AFTER", &cfg.StaleAfter},
		{"PUSH_REQUEST_TIMEOUT", &cfg.PushTimeout},
	} {
		value, err := requiredDuration(setting.name)
		if err != nil {
			return PositionConfig{}, err
		}
		*setting.out = value
	}
	for _, setting := range []struct {
		name string
		out  *int
	}{
		{"POSITION_WORKERS", &cfg.Workers},
		{"POSITION_QUEUE_SIZE", &cfg.QueueSize},
		{"POSITION_SCAN_BATCH_SIZE", &cfg.BatchSize},
	} {
		value, err := requiredInt32(setting.name)
		if err != nil {
			return PositionConfig{}, err
		}
		if value <= 0 {
			return PositionConfig{}, fmt.Errorf("%s must be greater than zero", setting.name)
		}
		*setting.out = int(value)
	}
	pushURL, err := requiredString("PUSH_HTTP_URL")
	if err != nil {
		return PositionConfig{}, err
	}
	parsed, err := url.Parse(pushURL)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return PositionConfig{}, fmt.Errorf("PUSH_HTTP_URL must be an HTTP or HTTPS base URL")
	}
	cfg.PushURL = pushURL
	return cfg, nil
}
