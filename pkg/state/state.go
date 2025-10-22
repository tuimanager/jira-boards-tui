package state

import (
	"encoding/json"
	"io"
	"os"
	"time"
)

type IssueState struct {
	Key            string    `json:"key"`
	Status         string    `json:"status"`
	Assignee       string    `json:"assignee"`
	LastUpdate     string    `json:"lastUpdate"`
	LastSeen       time.Time `json:"lastSeen"`
}

type BoardState struct {
	BoardID string                 `json:"boardId"`
	Issues  map[string]IssueState  `json:"issues"`
}

type AppState struct {
	Boards    map[string]BoardState `json:"boards"`
	LastRun   time.Time             `json:"lastRun"`
}

func LoadState(filename string) (*AppState, error) {
	file, err := os.Open(filename)
	if err != nil {
		// If file doesn't exist, return empty state
		if os.IsNotExist(err) {
			return &AppState{
				Boards:  make(map[string]BoardState),
				LastRun: time.Now(),
			}, nil
		}
		return nil, err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var state AppState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	return &state, nil
}

func (s *AppState) SaveState(filename string) error {
	s.LastRun = time.Now()
	
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}

func (s *AppState) GetBoardState(boardID string) BoardState {
	if board, exists := s.Boards[boardID]; exists {
		return board
	}
	
	// Create new board state
	board := BoardState{
		BoardID: boardID,
		Issues:  make(map[string]IssueState),
	}
	s.Boards[boardID] = board
	return board
}

func (s *AppState) UpdateIssueState(boardID, issueKey, status, assignee, lastUpdate string) {
	board := s.GetBoardState(boardID)
	
	board.Issues[issueKey] = IssueState{
		Key:        issueKey,
		Status:     status,
		Assignee:   assignee,
		LastUpdate: lastUpdate,
		LastSeen:   time.Now(),
	}
	
	s.Boards[boardID] = board
}

func (s *AppState) HasIssueChanged(boardID, issueKey, status, assignee string) bool {
	board := s.GetBoardState(boardID)
	
	if oldIssue, exists := board.Issues[issueKey]; exists {
		return oldIssue.Status != status || oldIssue.Assignee != assignee
	}
	
	// New issue is considered a change
	return true
}