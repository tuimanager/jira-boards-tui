package config

import (
	"encoding/json"
	"io"
	"os"
)

type Board struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Config struct {
	Boards          []Board `json:"boards"`
	RefreshInterval int     `json:"refreshInterval"`
	JiraURL         string  `json:"jiraURL"`
}

func LoadConfig(filename string) (*Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}