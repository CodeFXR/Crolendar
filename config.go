package main

import (
	"encoding/json"
	"os"
)

type JobMetadata struct {
	Alias string `json:"alias"`
	Color string `json:"color"`
}

// Config maps a job command (string) to its metadata
type Config struct {
	JobSettings map[string]JobMetadata `json:"job_settings"`
}

func LoadConfig() (Config, error) {
	var cfg Config
	cfg.JobSettings = make(map[string]JobMetadata)

	data, err := os.ReadFile("crolendar.json")
	if os.IsNotExist(err) {
		return cfg, nil // Return empty config if file doesn't exist
	}
	if err != nil {
		return cfg, err
	}

	err = json.Unmarshal(data, &cfg)
	return cfg, err
}

func SaveConfig(cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("crolendar.json", data, 0644)
}

// Helper to get display name and color for a job
func (c Config) GetJobDisplay(command string) (string, string) {
	if meta, ok := c.JobSettings[command]; ok {
		// Use alias if present, otherwise command
		display := meta.Alias
		if display == "" {
			display = command
		}
		
		// Use color if present, otherwise default to blue (#0052D4 from styles.go logic)
		// We'll return the hex code string
		color := meta.Color
		if color == "" {
			color = "#0052D4" // Default blue
		}
		return display, color
	}
	return command, "#0052D4"
}
