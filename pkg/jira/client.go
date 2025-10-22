package jira

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Constants removed - now configurable via client

type Client struct {
	httpClient *http.Client
	username   string
	password   string
	baseURL    string
	boardID    string
}

type Response struct {
	Issues []Issue `json:"issues"`
}

type Issue struct {
	Key        string      `json:"key"`
	Fields     IssueFields `json:"fields"`
	Changelog  *Changelog  `json:"changelog,omitempty"`
}

type IssueFields struct {
	Summary     string        `json:"summary"`
	Status      Status        `json:"status"`
	Description string        `json:"description"`
	DueDate     string        `json:"duedate"`
	Assignee    *Assignee     `json:"assignee"`
	Created     string        `json:"created"`
	Updated     string        `json:"updated"`
	Priority    *Priority     `json:"priority,omitempty"`
	IssueType   *IssueType    `json:"issuetype,omitempty"`
	Reporter    *Reporter     `json:"reporter,omitempty"`
	Comment     *CommentBlock `json:"comment,omitempty"`
}

type Priority struct {
	Name string `json:"name"`
}

type IssueType struct {
	Name string `json:"name"`
}

type Reporter struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type CommentBlock struct {
	Comments []Comment `json:"comments"`
}

type Comment struct {
	ID      string       `json:"id"`
	Body    string       `json:"body"`
	Author  CommentUser  `json:"author"`
	Created string       `json:"created"`
	Updated string       `json:"updated"`
}

type CommentUser struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type Status struct {
	Name string `json:"name"`
}

type Assignee struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type Changelog struct {
	Histories []History `json:"histories"`
}

type History struct {
	Created string        `json:"created"`
	Author  Author        `json:"author"`
	Items   []HistoryItem `json:"items"`
}

type Author struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type HistoryItem struct {
	Field      string `json:"field"`
	FieldType  string `json:"fieldtype"`
	From       string `json:"from"`
	FromString string `json:"fromString"`
	To         string `json:"to"`
	ToString   string `json:"toString"`
}

type SprintResponse struct {
	Values []Sprint `json:"values"`
}

type Sprint struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`
	StartDate string `json:"startDate,omitempty"`
	EndDate   string `json:"endDate,omitempty"`
}

func NewClient(username, password, baseURL string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		username:   username,
		password:   password,
		baseURL:    baseURL,
		boardID:    "", // Will be set via config
	}
}

func (c *Client) SetBoardID(boardID string) {
	c.boardID = boardID
}

func (c *Client) GetBoardID() string {
	return c.boardID
}

func (c *Client) makeRequest(method, endpoint string, body []byte) ([]byte, error) {
	url := c.baseURL + endpoint
	
	req, err := http.NewRequest(method, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func (c *Client) GetActiveSprintID() (int, error) {
	endpoint := fmt.Sprintf("/rest/agile/1.0/board/%s/sprint?state=active&maxResults=1", c.boardID)
	
	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("getting active sprint: %w", err)
	}

	var sprintResponse SprintResponse
	if err := json.Unmarshal(body, &sprintResponse); err != nil {
		return 0, fmt.Errorf("parsing sprint response: %w", err)
	}

	if len(sprintResponse.Values) == 0 {
		return 0, fmt.Errorf("no active sprints found")
	}

	return sprintResponse.Values[0].ID, nil
}

func (c *Client) GetSprintDetails(sprintID int) (*Sprint, error) {
	endpoint := fmt.Sprintf("/rest/agile/1.0/sprint/%d", sprintID)
	
	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("getting sprint details: %w", err)
	}

	var sprint Sprint
	if err := json.Unmarshal(body, &sprint); err != nil {
		return nil, fmt.Errorf("parsing sprint details: %w", err)
	}

	return &sprint, nil
}

func (c *Client) GetAllActiveSprints() ([]Sprint, error) {
	endpoint := fmt.Sprintf("/rest/agile/1.0/board/%s/sprint?state=active&maxResults=50", c.boardID)
	
	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("getting active sprints: %w", err)
	}

	var sprintResponse SprintResponse
	if err := json.Unmarshal(body, &sprintResponse); err != nil {
		return nil, fmt.Errorf("parsing sprints response: %w", err)
	}

	return sprintResponse.Values, nil
}

func (c *Client) GetAllSprints() ([]Sprint, error) {
	// Получаем активные спринты
	activeSprints, err := c.GetAllActiveSprints()
	if err != nil {
		return nil, err
	}
	
	// Получаем закрытые спринты
	endpoint := fmt.Sprintf("/rest/agile/1.0/board/%s/sprint?state=closed&maxResults=100", c.boardID)
	
	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("getting closed sprints: %w", err)
	}

	var closedSprintResponse SprintResponse
	if err := json.Unmarshal(body, &closedSprintResponse); err != nil {
		return nil, fmt.Errorf("parsing closed sprints response: %w", err)
	}
	
	// Объединяем все спринты
	allSprints := make([]Sprint, 0, len(activeSprints)+len(closedSprintResponse.Values))
	allSprints = append(allSprints, activeSprints...)
	allSprints = append(allSprints, closedSprintResponse.Values...)
	
	return allSprints, nil
}

func (c *Client) GetSprintIssues(sprintID int) ([]Issue, error) {
	endpoint := fmt.Sprintf("/rest/agile/1.0/board/%s/sprint/%s/issue?expand=changelog,comment&fields=*all&sort=status", c.boardID, strconv.Itoa(sprintID))
	
	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("getting sprint issues: %w", err)
	}

	var response Response
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parsing issues response: %w", err)
	}

	return response.Issues, nil
}

func (c *Client) GetIssueHistory(issueKey string) (*Issue, error) {
	endpoint := fmt.Sprintf("/rest/api/latest/issue/%s?expand=changelog", issueKey)
	
	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("getting issue history for %s: %w", issueKey, err)
	}

	var issue Issue
	if err := json.Unmarshal(body, &issue); err != nil {
		return nil, fmt.Errorf("parsing issue response: %w", err)
	}

	return &issue, nil
}

func (c *Client) GetBacklogIssues() ([]Issue, error) {
	endpoint := fmt.Sprintf("/rest/api/latest/search?jql=status=open")
	
	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("getting backlog issues: %w", err)
	}

	var response Response
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parsing backlog response: %w", err)
	}

	return response.Issues, nil
}

func (c *Client) SearchIssue(issueKey string) (*Issue, error) {
	endpoint := fmt.Sprintf("/rest/api/latest/search?jql=key=%s&expand=changelog,comment&fields=*all", issueKey)
	
	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("searching issue %s: %w", issueKey, err)
	}

	var response Response
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parsing search response: %w", err)
	}

	if len(response.Issues) == 0 {
		return nil, fmt.Errorf("issue %s not found", issueKey)
	}

	return &response.Issues[0], nil
}

func (c *Client) GetSprintIssuesViaJQL(sprintID int) ([]Issue, error) {
	// Используем JQL поиск для получения всех задач спринта - более надежный метод, без фильтрации по проекту
	endpoint := fmt.Sprintf("/rest/api/latest/search?jql=sprint=%d&expand=changelog,comment&fields=*all&maxResults=200", sprintID)
	
	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("searching sprint issues via JQL: %w", err)
	}

	var response Response
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parsing JQL search response: %w", err)
	}

	return response.Issues, nil
}