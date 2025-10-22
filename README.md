# Jira Boards TUI

A terminal user interface for monitoring Jira boards and sprint activities in real-time.

## Features

- Monitor multiple Jira boards simultaneously
- Real-time sprint activity tracking with change detection  
- Visual task organization with status columns
- Automatic refresh with configurable intervals
- Red highlighting for new changes (2-hour duration)
- Vim-style navigation (h/j/k/l)
- Board switching with number keys (1-9)
- Support for both Jira Server and Cloud instances

## Installation

```bash
go install github.com/your-username/jira-boards-tui@latest
```

Or build from source:
```bash
git clone https://github.com/your-username/jira-boards-tui.git
cd jira-boards-tui
go build
```

## Configuration

Create a `config.json` file:

```json
{
  "boards": [
    {
      "id": "123",
      "name": "Development",
      "description": "Development team board"
    },
    {
      "id": "456", 
      "name": "Testing",
      "description": "QA and testing board"
    }
  ],
  "refreshInterval": 60,
  "jiraURL": "https://your-company.atlassian.net"
}
```

## Authentication

Set your credentials as environment variables:

### Jira Cloud
```bash
export JIRA_USERNAME="your-email@company.com"
export JIRA_PASSWORD="your-api-token"
```

### Jira Server  
```bash
export JIRA_USERNAME="your-username"
export JIRA_PASSWORD="your-password"
```

## Usage

```bash
jira-boards-tui -tui
```

### Command line options
- `-config`: Path to configuration file (default: config.json)
- `-username`: Jira username (overrides env var)
- `-password`: Jira password (overrides env var)  
- `-tui`: Run in TUI mode

## Navigation

- **1-9**: Switch between configured boards
- **h/j/k/l**: Vim-style navigation within views
- **Ctrl+R**: Manual refresh
- **Ctrl+C**: Quit application

## Interface Layout

The TUI displays:
- **Header**: Current board info, navigation help, last refresh time
- **Status Columns**: Tasks organized by status (Open, Blocked, In Progress, Code Review, Ready for Test, In Testing, Tested, Done)
- **Activity Panel**: Recent changes and historical sprint activity

## Change Detection

- New changes are highlighted in red for 2 hours
- Changes automatically move from "Recent Changes" to "Historical Activity" after 2 hours
- Auto-switching to boards with new changes
- State persistence between application runs

## Status Mapping

The application maps various Jira statuses to standard columns:
- **Open**: Open, To Do, Backlog
- **Blocked**: Blocked, Impediment  
- **In Progress**: In Progress, Development
- **Code Review**: Code Review, Review
- **Ready for Test**: Ready for Test, RFT
- **In Testing**: In Testing, Testing
- **Tested**: Tested, Verified
- **Done**: Done, Resolved

## Requirements

- Go 1.21 or later
- Access to Jira API
- Terminal with color support

## License

MIT License