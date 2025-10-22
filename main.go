package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

func main() {
	var (
		configPath = flag.String("config", "config.json", "Path to configuration file")
		username   = flag.String("username", "", "Jira username")
		password   = flag.String("password", "", "Jira password")
		tuiMode    = flag.Bool("tui", false, "Run in TUI mode")
	)
	flag.Parse()

	// Get credentials from environment variables or command line flags
	if *username == "" {
		*username = os.Getenv("JIRA_USERNAME")
	}
	if *password == "" {
		*password = os.Getenv("JIRA_PASSWORD")
	}

	// Validate required credentials
	if *username == "" || *password == "" {
		fmt.Println("Error: JIRA credentials are required")
		fmt.Println("Set JIRA_USERNAME and JIRA_PASSWORD environment variables")
		fmt.Println("or use -username and -password flags")
		os.Exit(1)
	}

	if *tuiMode {
		app, err := NewTUIApp(*configPath, *username, *password)
		if err != nil {
			log.Fatal(err)
		}
		
		if err := app.Run(); err != nil {
			log.Fatal(err)
		}
	} else {
		// Keep legacy CLI mode for compatibility
		fmt.Println("Use -tui flag to run in TUI mode")
		fmt.Println("Legacy CLI mode is deprecated")
	}
}