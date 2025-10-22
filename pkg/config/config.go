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

type StatusMapping struct {
	Column   string   `json:"column"`
	Statuses []string `json:"statuses"`
}

type Workflow struct {
	Columns       []string        `json:"columns"`
	StatusMapping []StatusMapping `json:"statusMapping"`
	AutoDetect    bool            `json:"autoDetect"`
}

type Config struct {
	Boards          []Board  `json:"boards"`
	RefreshInterval int      `json:"refreshInterval"`
	JiraURL         string   `json:"jiraURL"`
	Workflow        Workflow `json:"workflow"`
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

	// Set default workflow if not configured
	if len(config.Workflow.Columns) == 0 {
		config.setDefaultWorkflow()
	}

	return &config, nil
}

func (c *Config) setDefaultWorkflow() {
	c.Workflow = Workflow{
		Columns: []string{
			"Open", "Blocked", "In Progress", "Code Review", 
			"Ready for Test", "In Testing", "Tested", "Done",
		},
		StatusMapping: []StatusMapping{
			{Column: "Open", Statuses: []string{"Open", "To Do", "Backlog", "Reopen", "Reopened"}},
			{Column: "Blocked", Statuses: []string{"Blocked"}},
			{Column: "In Progress", Statuses: []string{"In Progress", "In Development"}},
			{Column: "Code Review", Statuses: []string{"Code Review", "Review", "Pull Request"}},
			{Column: "Ready for Test", Statuses: []string{"Ready for Test", "Ready for Testing", "QA Ready"}},
			{Column: "In Testing", Statuses: []string{"In Testing", "Testing", "QA"}},
			{Column: "Tested", Statuses: []string{"Tested", "QA Done", "QA Complete"}},
			{Column: "Done", Statuses: []string{"Done", "Resolved", "Complete"}},
		},
		AutoDetect: false,
	}
}