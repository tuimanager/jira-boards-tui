package main

import (
	"fmt"
	"jira-boards-tui/pkg/config"
	"jira-boards-tui/pkg/jira"
	"jira-boards-tui/pkg/state"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jroimartin/gocui"
)

type ChangeNotification struct {
	BoardID   string
	IssueKey  string
	Summary   string // Full issue title/summary
	Change    string
	Timestamp time.Time
	IsNew     bool // true if change is new and should be highlighted in red
}

type TUIApp struct {
	gui               *gocui.Gui
	config            *config.Config
	jiraClient        *jira.Client
	currentBoard      int
	boardData         map[string][]jira.Issue
	mutex             sync.Mutex
	changes           []string
	changeQueue       []ChangeNotification // Queue of changes to highlight
	lastUpdate        time.Time
	activeViews       []string // Track currently visible views for navigation
	appState          *state.AppState
	stateFile         string
	autoSwitchEnabled bool
	boardSwitchTime   time.Time // Time when user last switched to current board
}

func NewTUIApp(configPath string, username, password string) (*TUIApp, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	client := jira.NewClient(username, password, cfg.JiraURL)
	
	// Load application state
	stateFile := "jira-summary-state.json"
	appState, err := state.LoadState(stateFile)
	if err != nil {
		// Create default state if loading fails
		appState = &state.AppState{
			Boards:  make(map[string]state.BoardState),
			LastRun: time.Now(),
		}
	}
	
	app := &TUIApp{
		config:            cfg,
		jiraClient:        client,
		boardData:         make(map[string][]jira.Issue),
		changes:           make([]string, 0),
		changeQueue:       make([]ChangeNotification, 0),
		lastUpdate:        time.Now(),
		appState:          appState,
		stateFile:         stateFile,
		autoSwitchEnabled: true,
		boardSwitchTime:   time.Now(),
	}

	g, err := gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		return nil, err
	}
	app.gui = g

	g.Highlight = true
	g.Cursor = true
	g.SelFgColor = gocui.ColorWhite
	g.SelBgColor = gocui.ColorDefault
	g.BgColor = gocui.ColorDefault
	g.FgColor = gocui.ColorWhite
	g.SetManagerFunc(app.layout)

	if err := app.setupKeyBindings(); err != nil {
		return nil, err
	}

	return app, nil
}

func (app *TUIApp) setupKeyBindings() error {
	g := app.gui

	// Quit
	if err := g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, app.quit); err != nil {
		return err
	}

	// Manual refresh
	if err := g.SetKeybinding("", gocui.KeyCtrlR, gocui.ModNone, app.refresh); err != nil {
		return err
	}

	// Board switching (1-9 keys) - fix closure issue
	// Add +1 for summary view
	maxBoards := len(app.config.Boards) + 1
	for i := 0; i < 9 && i < maxBoards; i++ {
		key := rune('1' + i)
		// Create closure to capture correct index
		func(boardIndex int) {
			g.SetKeybinding("", key, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
				return app.switchBoard(boardIndex)
			})
		}(i)
	}

	// Vim-style navigation keys for all views
	views := []string{"ready_for_test", "in_testing", "summary", "changelog", "global_summary"}
	for i := 0; i < 10; i++ {
		views = append(views, fmt.Sprintf("status_%d", i))
	}
	
	for _, viewName := range views {
		// Vertical navigation
		// Ignore binding errors - they happen when views don't exist yet
		g.SetKeybinding(viewName, 'j', gocui.ModNone, app.cursorDown)
		g.SetKeybinding(viewName, 'k', gocui.ModNone, app.cursorUp)
		
		// Horizontal navigation between views
		g.SetKeybinding(viewName, 'h', gocui.ModNone, app.moveToPreviousView)
		g.SetKeybinding(viewName, 'l', gocui.ModNone, app.moveToNextView)
		
		// Page navigation
		g.SetKeybinding(viewName, gocui.KeyCtrlF, gocui.ModNone, app.pageDown)
		g.SetKeybinding(viewName, gocui.KeyCtrlB, gocui.ModNone, app.pageUp)
		
		// Go to top/bottom
		g.SetKeybinding(viewName, 'g', gocui.ModNone, app.goToTop)
		g.SetKeybinding(viewName, 'G', gocui.ModNone, app.goToBottom)
	}
	
	// Tab navigation
	if err := g.SetKeybinding("", gocui.KeyTab, gocui.ModNone, app.moveToNextView); err != nil {
		return err
	}

	return nil
}

func (app *TUIApp) layout(g *gocui.Gui) error {
	app.mutex.Lock()
	defer app.mutex.Unlock()

	maxX, maxY := g.Size()

	// Header with board info and navigation
	if v, err := g.SetView("header", 0, 0, maxX-1, 2); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Frame = true
		v.Title = "Jira Summary TUI"
	}
	// Always update header to show current timestamp
	if headerView, err := g.View("header"); err == nil {
		app.updateHeader(headerView)
	}

	// Clear all views properly
	viewsToDelete := []string{"loading", "summary", "changelog", "global_summary"}
	for i := 0; i < 10; i++ {
		viewsToDelete = append(viewsToDelete, fmt.Sprintf("status_%d", i))
	}
	
	for _, viewName := range viewsToDelete {
		if v, err := g.View(viewName); err == nil && v != nil {
			g.DeleteView(viewName)
		}
	}
	
	// Reset active views list
	app.activeViews = []string{}

	// Main content area - show current board stats or global summary
	if app.currentBoard == len(app.config.Boards) {
		// Global summary view
		if v, err := g.SetView("global_summary", 0, 3, maxX-1, maxY-1); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			v.Title = "Global Statistics - All Boards"
			v.Wrap = false
			v.BgColor = gocui.ColorDefault
			v.FgColor = gocui.ColorWhite
			app.updateGlobalSummaryView(v)
		}
		app.activeViews = append(app.activeViews, "global_summary")
	} else if app.currentBoard < len(app.config.Boards) {
		boardID := app.config.Boards[app.currentBoard].ID
		issues := app.boardData[boardID]

		// Get all statuses except "Closed"
		statuses := app.getAllStatuses(issues)
		statusCount := len(statuses)
		
		if statusCount > 0 {
			// Use left 2/3 of screen for status columns, right 1/3 for activity
			leftWidth := (maxX * 2) / 3
			colWidth := leftWidth / statusCount
			
			for i, status := range statuses {
				x1 := i * colWidth
				x2 := (i + 1) * colWidth - 1
				if i == statusCount-1 {
					x2 = leftWidth - 1 // Last column takes remaining left space
				}
				
				viewName := fmt.Sprintf("status_%d", i)
				if v, err := g.SetView(viewName, x1, 3, x2, maxY-1); err != nil {
					if err != gocui.ErrUnknownView {
						return err
					}
					v.Title = status
					v.Highlight = true
					v.SelBgColor = gocui.ColorDefault
					v.SelFgColor = gocui.ColorWhite
					v.BgColor = gocui.ColorDefault
					v.FgColor = gocui.ColorWhite
					app.updateTaskView(v, issues, status)
				}
				app.activeViews = append(app.activeViews, viewName)
			}
		} else {
			// No issues loaded yet - use left 2/3 of screen
			leftWidth := (maxX * 2) / 3
			if v, err := g.SetView("loading", 0, 3, leftWidth-1, maxY-1); err != nil {
				if err != gocui.ErrUnknownView {
					return err
				}
				v.Title = "Loading..."
				v.BgColor = gocui.ColorDefault
				v.FgColor = gocui.ColorWhite
				fmt.Fprintln(v, "Loading issues...")
			}
		}

		// Sprint changelog view - now takes right 1/3 of screen
		leftWidth := (maxX * 2) / 3
		if v, err := g.SetView("changelog", leftWidth, 3, maxX-1, maxY-1); err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			v.Title = fmt.Sprintf("Sprint Activity - Board: %s", app.config.Boards[app.currentBoard].Name)
			v.Wrap = false
			v.BgColor = gocui.ColorDefault
			v.FgColor = gocui.ColorWhite
			v.Autoscroll = true
			app.updateSprintChangelog(v, issues)
		}
		app.activeViews = append(app.activeViews, "changelog")
	}

	// Remove Changes view - all changes are now shown in activity

	return nil
}

func (app *TUIApp) getAllStatuses(issues []jira.Issue) []string {
	// Auto-detect workflow if enabled
	if app.config.Workflow.AutoDetect {
		return app.autoDetectWorkflow(issues)
	}
	
	// Use configured columns
	wantedStatuses := app.config.Workflow.Columns
	
	// Check which statuses actually exist in the data (with mapping)
	statusExists := make(map[string]bool)
	for _, issue := range issues {
		mappedStatus := app.mapStatusToGroup(issue.Fields.Status.Name)
		statusExists[mappedStatus] = true
	}
	
	// Return only statuses that exist in the data, in our preferred order
	var existingStatuses []string
	for _, status := range wantedStatuses {
		if statusExists[status] {
			existingStatuses = append(existingStatuses, status)
		}
	}
	
	return existingStatuses
}

func (app *TUIApp) mapStatusToGroup(status string) string {
	// Use configured status mapping
	for _, mapping := range app.config.Workflow.StatusMapping {
		for _, mappedStatus := range mapping.Statuses {
			if status == mappedStatus {
				return mapping.Column
			}
		}
	}
	
	// Special case for Closed - always filter out
	if status == "Closed" {
		return "Closed"
	}
	
	// Keep original status if no mapping found
	return status
}

func (app *TUIApp) autoDetectWorkflow(issues []jira.Issue) []string {
	// Collect all unique statuses from issues
	statusCount := make(map[string]int)
	for _, issue := range issues {
		if issue.Fields.Status.Name != "Closed" {
			statusCount[issue.Fields.Status.Name]++
		}
	}
	
	// Sort statuses by frequency (most common first)
	type statusFreq struct {
		Status string
		Count  int
	}
	
	var statuses []statusFreq
	for status, count := range statusCount {
		statuses = append(statuses, statusFreq{Status: status, Count: count})
	}
	
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].Count > statuses[j].Count
	})
	
	// Return just the status names
	var result []string
	for _, s := range statuses {
		result = append(result, s.Status)
	}
	
	return result
}

func (app *TUIApp) updateHeader(v *gocui.View) {
	v.Clear()
	if app.currentBoard == len(app.config.Boards) {
		// Summary view
		fmt.Fprintf(v, "Summary View | Press 1-%d to switch | Ctrl+R refresh | Last: %s",
			len(app.config.Boards)+1, app.lastUpdate.Format("15:04:05"))
	} else if app.currentBoard >= 0 && app.currentBoard < len(app.config.Boards) {
		board := app.config.Boards[app.currentBoard]
		fmt.Fprintf(v, "Board: %s (%s) | Press 1-%d to switch | Ctrl+R refresh | Last: %s",
			board.Name, board.ID, len(app.config.Boards)+1, app.lastUpdate.Format("15:04:05"))
	}
}

func (app *TUIApp) updateTaskView(v *gocui.View, issues []jira.Issue, status string) {
	v.Clear()
	
	// Debug output
	if len(issues) == 0 {
		fmt.Fprintf(v, "No issues loaded yet...\n")
		return
	}
	
	found := false
	for _, issue := range issues {
		mappedStatus := app.mapStatusToGroup(issue.Fields.Status.Name)
		if mappedStatus == status {
			found = true
			priority := ""
			if issue.Fields.Priority != nil {
				priority = issue.Fields.Priority.Name
			}
			assignee := "Unassigned"
			if issue.Fields.Assignee != nil {
				assignee = issue.Fields.Assignee.Name
			}
			
			summary := issue.Fields.Summary
			if len(summary) > 20 {
				summary = summary[:20] + "..."
			}
			
			// Check if this issue has recent changes (should be highlighted in red)
			isNewChange := app.isIssueNewChange(issue.Key)
			
			// Show original status in parentheses for clarity
			originalStatus := issue.Fields.Status.Name
			line := ""
			if originalStatus != status {
				line = fmt.Sprintf("%s | %s | %s | %s (%s)", issue.Key, priority, assignee, summary, originalStatus)
			} else {
				line = fmt.Sprintf("%s | %s | %s | %s", issue.Key, priority, assignee, summary)
			}
			
			// Add red highlighting for new changes
			if isNewChange {
				line = "\033[31m" + line + "\033[0m" // Red ANSI color
			}
			
			fmt.Fprintln(v, line)
		}
	}
	
	if !found {
		fmt.Fprintf(v, "No issues with status: %s\n", status)
	}
}

func (app *TUIApp) isIssueNewChange(issueKey string) bool {
	// Get current board ID
	currentBoardID := ""
	if app.currentBoard >= 0 && app.currentBoard < len(app.config.Boards) {
		currentBoardID = app.config.Boards[app.currentBoard].ID
	}
	
	for _, change := range app.changeQueue {
		if change.IssueKey == issueKey && change.IsNew {
			// Filter by current board if we have a board selected
			if currentBoardID == "" || change.BoardID == currentBoardID {
				return true
			}
		}
	}
	return false
}

func (app *TUIApp) updateSummaryView(v *gocui.View, issues []jira.Issue) {
	v.Clear()
	
	if len(issues) == 0 {
		fmt.Fprintln(v, "No issues to display")
		return
	}
	
	stats := make(map[string]map[string]int)
	
	for _, issue := range issues {
		assignee := "Unassigned"
		if issue.Fields.Assignee != nil {
			assignee = issue.Fields.Assignee.Name
		}
		
		if stats[assignee] == nil {
			stats[assignee] = make(map[string]int)
		}
		
		mappedStatus := app.mapStatusToGroup(issue.Fields.Status.Name)
		// Include all statuses except Closed
		if mappedStatus != "Closed" {
			stats[assignee][mappedStatus]++
			stats[assignee]["Total"]++
		}
	}
	
	fmt.Fprintln(v, "Assignee | Open | Blocked | Progress | Review | RFT | Testing | Tested | Done | Total")
	fmt.Fprintln(v, strings.Repeat("-", 80))
	
	for assignee, statusMap := range stats {
		open := statusMap["Open"]
		blocked := statusMap["Blocked"]
		progress := statusMap["In Progress"] 
		review := statusMap["Code Review"]
		rft := statusMap["Ready for Test"]
		testing := statusMap["In Testing"]
		tested := statusMap["Tested"]
		done := statusMap["Done"]
		total := statusMap["Total"]
		fmt.Fprintf(v, "%s | %d | %d | %d | %d | %d | %d | %d | %d | %d\n", 
			assignee, open, blocked, progress, review, rft, testing, tested, done, total)
	}
}

func (app *TUIApp) updateGlobalSummaryView(v *gocui.View) {
	v.Clear()
	
	globalStats := make(map[string]map[string]int)
	
	// Aggregate data from all boards
	for _, issues := range app.boardData {
		for _, issue := range issues {
			assignee := "Unassigned"
			if issue.Fields.Assignee != nil {
				assignee = issue.Fields.Assignee.Name
			}
			
			if globalStats[assignee] == nil {
				globalStats[assignee] = make(map[string]int)
			}
			
			mappedStatus := app.mapStatusToGroup(issue.Fields.Status.Name)
			// Include all statuses except Closed
			if mappedStatus != "Closed" {
				globalStats[assignee][mappedStatus]++
				globalStats[assignee]["Total"]++
			}
		}
	}
	
	fmt.Fprintln(v, "Global Statistics Across All Boards")
	fmt.Fprintln(v, "")
	fmt.Fprintln(v, "Assignee | Open | Blocked | Progress | Review | RFT | Testing | Tested | Done | Total")
	fmt.Fprintln(v, strings.Repeat("-", 80))
	
	for assignee, statusMap := range globalStats {
		open := statusMap["Open"]
		blocked := statusMap["Blocked"]
		progress := statusMap["In Progress"] 
		review := statusMap["Code Review"]
		rft := statusMap["Ready for Test"]
		testing := statusMap["In Testing"]
		tested := statusMap["Tested"]
		done := statusMap["Done"]
		total := statusMap["Total"]
		fmt.Fprintf(v, "%s | %d | %d | %d | %d | %d | %d | %d | %d | %d\n", 
			assignee, open, blocked, progress, review, rft, testing, tested, done, total)
	}
}

func (app *TUIApp) updateSprintChangelog(v *gocui.View, issues []jira.Issue) {
	v.Clear()
	
	if len(issues) == 0 {
		fmt.Fprintln(v, "No issues to analyze")
		return
	}
	
	// Get current board ID
	currentBoardID := ""
	if app.currentBoard >= 0 && app.currentBoard < len(app.config.Boards) {
		currentBoardID = app.config.Boards[app.currentBoard].ID
	}

	// Show recent changes from our change queue first (only for current board)
	fmt.Fprintln(v, "Recent Changes (Since Last Run):")
	fmt.Fprintln(v, strings.Repeat("-", 80))
	
	if len(app.changeQueue) > 0 {
		// Show latest changes first, filtered by current board and within 2 hours
		recentCutoff := time.Now().Add(-2 * time.Hour)
		count := 0
		for i := len(app.changeQueue) - 1; i >= 0 && count < 10; i-- {
			change := app.changeQueue[i]
			
			// Skip changes from other boards
			if currentBoardID != "" && change.BoardID != currentBoardID {
				continue
			}
			
			// Skip changes older than 2 hours - they will appear in Historical Activity
			if change.Timestamp.Before(recentCutoff) {
				continue
			}
			
			timeStr := change.Timestamp.Format("15:04:05")
			
			line := fmt.Sprintf("[%s] %s: %s - %s", timeStr, change.IssueKey, change.Summary, change.Change)
			
			// Highlight new changes in red
			if change.IsNew {
				line = "\033[31m" + line + "\033[0m"
			}
			
			fmt.Fprintln(v, line)
			count++
		}
	}
	
	// Check if there are any recent changes for current board (within 2 hours)
	hasRecentBoardChanges := false
	recentCutoff := time.Now().Add(-2 * time.Hour)
	for _, change := range app.changeQueue {
		if (currentBoardID == "" || change.BoardID == currentBoardID) && change.Timestamp.After(recentCutoff) {
			hasRecentBoardChanges = true
			break
		}
	}
	
	if !hasRecentBoardChanges {
		fmt.Fprintln(v, "No new changes in last 2 hours for this board")
	}
	
	fmt.Fprintln(v, "")
	fmt.Fprintln(v, "Historical Sprint Activity:")
	fmt.Fprintln(v, strings.Repeat("-", 80))
	
	// Collect recent activities from Jira changelog
	type Activity struct {
		Time   string
		Type   string
		Issue  string
		Detail string
	}
	
	var activities []Activity
	
	// First add older changes from our change queue (older than 2 hours but within 24 hours)
	recentCutoff = time.Now().Add(-2 * time.Hour)
	oldCutoff := time.Now().Add(-24 * time.Hour)
	
	for _, change := range app.changeQueue {
		// Skip changes from other boards
		if currentBoardID != "" && change.BoardID != currentBoardID {
			continue
		}
		
		// Add changes that are older than 2 hours but newer than 24 hours
		if change.Timestamp.Before(recentCutoff) && change.Timestamp.After(oldCutoff) {
			activity := Activity{
				Time:   change.Timestamp.Format("2006-01-02T15:04:05.000-0700"),
				Type:   "Change",
				Issue:  change.IssueKey,
				Detail: fmt.Sprintf("%s - %s", change.Summary, change.Change),
			}
			activities = append(activities, activity)
		}
	}
	
	// Analyze changelog for each issue (only recent activities)
	cutoff := time.Now().Add(-24 * time.Hour) // Last 24 hours
	
	for _, issue := range issues {
		if issue.Changelog != nil {
			for _, history := range issue.Changelog.Histories {
				// Parse timestamp
				historyTime, err := time.Parse("2006-01-02T15:04:05.000-0700", history.Created)
				if err != nil {
					continue
				}
				
				// Only show recent activities
				if historyTime.Before(cutoff) {
					continue
				}
				
				for _, item := range history.Items {
					if item.Field == "status" {
						activity := Activity{
							Time:   history.Created,
							Type:   "Status",
							Issue:  issue.Key,
							Detail: fmt.Sprintf("%s → %s by %s", item.FromString, item.ToString, history.Author.DisplayName),
						}
						activities = append(activities, activity)
					}
					if item.Field == "assignee" {
						activity := Activity{
							Time:   history.Created,
							Type:   "Assignee",
							Issue:  issue.Key,
							Detail: fmt.Sprintf("assigned to %s by %s", item.ToString, history.Author.DisplayName),
						}
						activities = append(activities, activity)
					}
				}
			}
		}
		
		// Add recent comments
		if issue.Fields.Comment != nil {
			for _, comment := range issue.Fields.Comment.Comments {
				commentTime, err := time.Parse("2006-01-02T15:04:05.000-0700", comment.Created)
				if err != nil {
					continue
				}
				
				if commentTime.Before(cutoff) {
					continue
				}
				
				activity := Activity{
					Time:   comment.Created,
					Type:   "Comment",
					Issue:  issue.Key,
					Detail: fmt.Sprintf("commented by %s", comment.Author.DisplayName),
				}
				activities = append(activities, activity)
			}
		}
	}
	
	// Sort activities by time (newest first)
	sort.Slice(activities, func(i, j int) bool {
		timeI, errI := time.Parse("2006-01-02T15:04:05.000-0700", activities[i].Time)
		timeJ, errJ := time.Parse("2006-01-02T15:04:05.000-0700", activities[j].Time)
		if errI != nil || errJ != nil {
			return false
		}
		return timeI.After(timeJ) // Newer activities first
	})
	
	// Limit to 20 historical activities
	if len(activities) > 20 {
		activities = activities[:20]
	}
	
	for _, activity := range activities {
		timeStr := activity.Time
		if len(timeStr) > 19 {
			timeStr = timeStr[:19] // Keep YYYY-MM-DD HH:MM:SS part
		}
		fmt.Fprintf(v, "[%s] %s %s: %s\n", timeStr, activity.Type, activity.Issue, activity.Detail)
	}
	
	if len(activities) == 0 {
		fmt.Fprintln(v, "No historical activity found in last 24h")
	}
}


func (app *TUIApp) switchBoard(boardIndex int) error {
	// Handle summary view (last tab)
	if boardIndex == len(app.config.Boards) {
		app.currentBoard = boardIndex
		// Force UI update
		app.gui.Update(func(g *gocui.Gui) error {
			return nil
		})
		return nil
	}
	
	if boardIndex >= 0 && boardIndex < len(app.config.Boards) {
		app.currentBoard = boardIndex
		app.boardSwitchTime = time.Now() // Record when user switched to this board
		board := app.config.Boards[boardIndex]
		
		app.jiraClient.SetBoardID(board.ID)
		
		// Start timer to turn red changes white after 1 minute on current board
		go app.startBoardViewTimer(board.ID)
		
		// Force refresh data for the new board
		go app.refreshBoardData(board.ID)
		
		// Force UI update immediately
		app.gui.Update(func(g *gocui.Gui) error {
			return nil
		})
	}
	return nil
}

func (app *TUIApp) startBoardViewTimer(boardID string) {
	// Red changes now last for 2 hours automatically via cleanupChangeQueue
	// No need for board-specific timer
}

func (app *TUIApp) refresh(g *gocui.Gui, v *gocui.View) error {
	go app.refreshAllData()
	return nil
}

func (app *TUIApp) refreshAllData() {
	for _, board := range app.config.Boards {
		app.refreshBoardData(board.ID)
	}
}

func (app *TUIApp) refreshBoardData(boardID string) {
	app.jiraClient.SetBoardID(boardID)
	
	sprints, err := app.jiraClient.GetAllActiveSprints()
	if err != nil {
		return
	}
	
	var allIssues []jira.Issue
	for _, sprint := range sprints {
		issues, err := app.jiraClient.GetSprintIssuesViaJQL(sprint.ID)
		if err != nil {
			continue
		}
		allIssues = append(allIssues, issues...)
	}
	
	app.mutex.Lock()
	app.boardData[boardID] = allIssues
	app.lastUpdate = time.Now()
	
	// Detect changes and update state
	hasNewChanges := app.detectAndStoreChanges(boardID, allIssues)
	
	// Auto-switch to board with new changes
	if hasNewChanges && app.autoSwitchEnabled {
		go app.switchToBoardWithChanges(boardID)
	}
	
	app.mutex.Unlock()
	
	// Save state
	app.appState.SaveState(app.stateFile)
	
	// Force UI update with complete redraw
	app.gui.Update(func(g *gocui.Gui) error {
		// Force redraw of all views
		for _, viewName := range app.activeViews {
			if v, err := g.View(viewName); err == nil {
				v.Clear()
			}
		}
		// Update header with new timestamp
		if headerView, err := g.View("header"); err == nil {
			app.updateHeader(headerView)
		}
		return nil
	})
}

func (app *TUIApp) detectAndStoreChanges(boardID string, issues []jira.Issue) bool {
	hasNewChanges := false
	
	for _, issue := range issues {
		assignee := "Unassigned"
		if issue.Fields.Assignee != nil {
			assignee = issue.Fields.Assignee.Name
		}
		
		status := issue.Fields.Status.Name
		lastUpdate := issue.Fields.Updated
		
		// Check if issue has changed since last run (only compare if we have previous state)
		if app.appState.HasIssueChanged(boardID, issue.Key, status, assignee) {
			// Only mark as new change if this is not the first run
			boardState := app.appState.GetBoardState(boardID)
			if len(boardState.Issues) > 0 { // We have previous state
				hasNewChanges = true
				
				
				// Add to change queue for red highlighting
				change := ChangeNotification{
					BoardID:   boardID,
					IssueKey:  issue.Key,
					Summary:   issue.Fields.Summary,
					Change:    fmt.Sprintf("Status: %s, Assignee: %s", status, assignee),
					Timestamp: time.Now(),
					IsNew:     true,
				}
				app.changeQueue = append(app.changeQueue, change)
				
				// Add to changes list for display
				changeText := fmt.Sprintf("[%s] %s: Status changed to %s", boardID, issue.Key, status)
				app.changes = append(app.changes, changeText)
				
				// Keep only last 20 changes
				if len(app.changes) > 20 {
					app.changes = app.changes[1:]
				}
			}
		}
		
		// Always update state
		app.appState.UpdateIssueState(boardID, issue.Key, status, assignee, lastUpdate)
	}
	
	// Clean up old change notifications (older than 2 hours become white)
	cutoff := time.Now().Add(-2 * time.Hour)
	for i := range app.changeQueue {
		if app.changeQueue[i].Timestamp.Before(cutoff) {
			app.changeQueue[i].IsNew = false
		}
	}
	
	// Remove very old notifications (older than 24 hours)
	oldCutoff := time.Now().Add(-24 * time.Hour)
	var filteredQueue []ChangeNotification
	for _, change := range app.changeQueue {
		if change.Timestamp.After(oldCutoff) {
			filteredQueue = append(filteredQueue, change)
		}
	}
	app.changeQueue = filteredQueue
	
	return hasNewChanges
}

func (app *TUIApp) cleanupChangeQueue() {
	app.mutex.Lock()
	defer app.mutex.Unlock()
	
	// Clean up old change notifications (older than 2 hours become white)
	cutoff := time.Now().Add(-2 * time.Hour)
	needsUpdate := false
	for i := range app.changeQueue {
		if app.changeQueue[i].IsNew && app.changeQueue[i].Timestamp.Before(cutoff) {
			app.changeQueue[i].IsNew = false
			needsUpdate = true
		}
	}
	
	// Remove very old notifications (older than 24 hours)
	oldCutoff := time.Now().Add(-24 * time.Hour)
	var filteredQueue []ChangeNotification
	for _, change := range app.changeQueue {
		if change.Timestamp.After(oldCutoff) {
			filteredQueue = append(filteredQueue, change)
		} else {
			needsUpdate = true
		}
	}
	app.changeQueue = filteredQueue
	
	// Update UI if changes were made
	if needsUpdate {
		app.gui.Update(func(g *gocui.Gui) error {
			return nil
		})
	}
}

func (app *TUIApp) switchToBoardWithChanges(boardID string) {
	// Find board index by ID
	for i, board := range app.config.Boards {
		if board.ID == boardID {
			time.Sleep(1 * time.Second) // Small delay before switching
			app.switchBoard(i)
			break
		}
	}
}

func (app *TUIApp) cursorDown(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		cx, cy := v.Cursor()
		if err := v.SetCursor(cx, cy+1); err != nil {
			ox, oy := v.Origin()
			if err := v.SetOrigin(ox, oy+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func (app *TUIApp) cursorUp(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		ox, oy := v.Origin()
		cx, cy := v.Cursor()
		if err := v.SetCursor(cx, cy-1); err != nil && oy > 0 {
			if err := v.SetOrigin(ox, oy-1); err != nil {
				return err
			}
		}
	}
	return nil
}

func (app *TUIApp) moveToPreviousView(g *gocui.Gui, v *gocui.View) error {
	if len(app.activeViews) == 0 {
		return nil
	}
	
	currentName := ""
	if v != nil {
		currentName = v.Name()
	}
	
	// Find current view index
	currentIndex := -1
	for i, viewName := range app.activeViews {
		if viewName == currentName {
			currentIndex = i
			break
		}
	}
	
	// Move to previous view (wrap around)
	nextIndex := currentIndex - 1
	if nextIndex < 0 {
		nextIndex = len(app.activeViews) - 1
	}
	
	if nextIndex >= 0 && nextIndex < len(app.activeViews) {
		nextViewName := app.activeViews[nextIndex]
		g.SetCurrentView(nextViewName)
	}
	
	return nil
}

func (app *TUIApp) moveToNextView(g *gocui.Gui, v *gocui.View) error {
	if len(app.activeViews) == 0 {
		return nil
	}
	
	currentName := ""
	if v != nil {
		currentName = v.Name()
	}
	
	// Find current view index
	currentIndex := -1
	for i, viewName := range app.activeViews {
		if viewName == currentName {
			currentIndex = i
			break
		}
	}
	
	// Move to next view (wrap around)
	nextIndex := (currentIndex + 1) % len(app.activeViews)
	
	if nextIndex < len(app.activeViews) {
		nextViewName := app.activeViews[nextIndex]
		g.SetCurrentView(nextViewName)
	}
	
	return nil
}

func (app *TUIApp) pageDown(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		_, maxY := v.Size()
		cx, cy := v.Cursor()
		ox, oy := v.Origin()
		
		newCY := cy + maxY
		newOY := oy
		
		// Try to move cursor first
		if err := v.SetCursor(cx, newCY); err != nil {
			// If cursor can't move, scroll the view
			newOY = oy + maxY
			v.SetOrigin(ox, newOY)
		}
	}
	return nil
}

func (app *TUIApp) pageUp(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		_, maxY := v.Size()
		cx, cy := v.Cursor()
		ox, oy := v.Origin()
		
		newCY := cy - maxY
		if newCY < 0 {
			newCY = 0
		}
		
		newOY := oy - maxY
		if newOY < 0 {
			newOY = 0
		}
		
		v.SetCursor(cx, newCY)
		v.SetOrigin(ox, newOY)
	}
	return nil
}

func (app *TUIApp) goToTop(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		v.SetCursor(0, 0)
		v.SetOrigin(0, 0)
	}
	return nil
}

func (app *TUIApp) goToBottom(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		// Get view content height
		lines := strings.Split(v.Buffer(), "\n")
		if len(lines) > 0 {
			_, maxY := v.Size()
			lastLine := len(lines) - 1
			
			if lastLine >= maxY {
				// Need to scroll
				v.SetOrigin(0, lastLine-maxY+1)
				v.SetCursor(0, maxY-1)
			} else {
				// Can fit on screen
				v.SetOrigin(0, 0)
				v.SetCursor(0, lastLine)
			}
		}
	}
	return nil
}

func (app *TUIApp) quit(g *gocui.Gui, v *gocui.View) error {
	// Save state before quitting
	app.appState.SaveState(app.stateFile)
	return gocui.ErrQuit
}

func (app *TUIApp) Run() error {
	defer func() {
		app.gui.Close()
		// Save state on exit
		app.appState.SaveState(app.stateFile)
	}()
	
	// Set initial board
	if len(app.config.Boards) > 0 {
		app.currentBoard = 0
		app.jiraClient.SetBoardID(app.config.Boards[0].ID)
	}
	
	// Initial data load
	go app.refreshAllData()
	
	// Auto-refresh timer
	ticker := time.NewTicker(time.Duration(app.config.RefreshInterval) * time.Second)
	go func() {
		for range ticker.C {
			app.refreshAllData()
		}
	}()
	defer ticker.Stop()
	
	// Timer for cleaning up change notifications (every 30 seconds)
	cleanupTicker := time.NewTicker(30 * time.Second)
	go func() {
		for range cleanupTicker.C {
			app.cleanupChangeQueue()
		}
	}()
	defer cleanupTicker.Stop()
	
	if err := app.gui.MainLoop(); err != nil && err != gocui.ErrQuit {
		return err
	}
	
	return nil
}