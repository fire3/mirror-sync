package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/user/mirror-sync/internal/config"
	"github.com/user/mirror-sync/internal/runner"
	"github.com/user/mirror-sync/internal/taskctrl"
	"github.com/user/mirror-sync/types"
)

// ── Screen enum ────────────────────────────────────────────────────────────

type Screen int

const (
	screenProvider Screen = iota
	screenTask
	screenConfig
	screenConfirm
	screenRunning
	screenDone
)

type ConfigSection int

const (
	configBase ConfigSection = iota
	configTask
)

// ── Task / field definitions ───────────────────────────────────────────────

type taskDef struct {
	id          types.PypiTaskType
	label       string
	description string
	fields      []fieldDef
}

type fieldDef struct {
	key, label string
}

var taskDefs = []taskDef{
	{
		id:          types.PypiTaskMetadataSync,
		label:       "下载元数据",
		description: "同步 PyPI simple 元数据 (HTML + JSON) 到本地。",
		fields:      nil, // no config needed — date is always today
	},
	{
		id:          types.PypiTaskArtifactDownload,
		label:       "按单日快照下载包",
		description: "根据指定日期的元数据快照，下载全部包文件。",
		fields: []fieldDef{
			{key: "metadataDate", label: "Source Date"},
			{key: "outputDate", label: "Output Date"},
		},
	},
	{
		id:          types.PypiTaskIncrementalDownload,
		label:       "增量下载 (两份快照对比)",
		description: "对比两个快照，只下载新增或变更的包。",
		fields: []fieldDef{
			{key: "oldMetadataDate", label: "Old Date"},
			{key: "newMetadataDate", label: "New Date"},
			{key: "outputDate", label: "Output Date"},
		},
	},
}

var baseFields = []fieldDef{
	{key: "profileName", label: "Profile"},
	{key: "simpleUrl", label: "Simple URL"},
	{key: "metadataRoot", label: "Metadata Root"},
	{key: "mirrorRoot", label: "Mirror Root"},
	{key: "concurrency", label: "Concurrency"},
	{key: "retry", label: "Retry"},
	{key: "timeoutMs", label: "Timeout (ms)"},
}

// ── Lipgloss styles ────────────────────────────────────────────────────────

var (
	// Core
	sBold      = lipgloss.NewStyle().Bold(true)
	sCyan      = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	sGreen     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	sYellow    = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	sRed       = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	sDim       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sWhite     = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	sGray      = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))

	// Composite
	sTitle      = sCyan.Bold(true)
	sSection    = sYellow.Bold(true)
	sHighlight  = sGreen.Bold(true)
	sSelected   = sGreen
	sError      = sRed
	sDisabled   = sDim
	sDimCyan    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	sPrompt     = sWhite.Bold(true)

	// Layout
	sBorder     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("14")).Padding(0, 1)
	sBox        = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("8")).Padding(0, 1)
	sGreenBox   = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("10")).Padding(0, 1)
)

var checkMark = sGreen.Render("✔")
var arrow = sGreen.Render("▸")
var spacer = " "

// ── Model ──────────────────────────────────────────────────────────────────

type model struct {
	cfg           types.AppConfig
	screen        Screen
	configSection ConfigSection

	taskIdx      int
	baseFieldIdx int
	taskFieldIdx int

	editingField string
	textInput    textinput.Model

	loading bool
	saving  bool
	status  string
	logs    []string

	running        bool
	lastResult     *types.SyncRunResult
	taskController *taskctrl.Controller
	progress       *runner.SyncProgress

	width  int
	height int
	ready  bool
}

func initialModel() model {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 60
	ti.Prompt = ""

	return model{
		cfg:           config.DefaultConfig(),
		screen:        screenProvider,
		configSection: configBase,
		textInput:     ti,
		loading:       true,
		status:        "Loading config...",
	}
}

func Run() error {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// ── Init ───────────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return tea.Batch(loadConfigCmd, textinput.Blink)
}

func loadConfigCmd() tea.Msg {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return configLoadedMsg{cfg: config.DefaultConfig()}
	}
	return configLoadedMsg{cfg: cfg}
}

type configLoadedMsg struct{ cfg types.AppConfig }
type saveDoneMsg struct{ err error }

// runStartedMsg carries channels for progress streaming.
type runStartedMsg struct {
	progCh <-chan runner.SyncEvent
	doneCh <-chan runDoneMsg
}

// progressMsg carries a single progress event from the runner.
type progressMsg struct {
	event runner.SyncEvent
	progCh <-chan runner.SyncEvent
	doneCh <-chan runDoneMsg
}

type runDoneMsg struct {
	result types.SyncRunResult
	err    error
}

// ── Update ─────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case configLoadedMsg:
		m.loading = false
		m.cfg = msg.cfg
		// Always reset dates to today
		m.resetDates()
		for i, td := range taskDefs {
			if td.id == m.cfg.SelectedTask {
				m.taskIdx = i
				break
			}
		}
		m.status = "Config loaded"
		return m, nil

	case saveDoneMsg:
		m.saving = false
		if msg.err != nil {
			m.status = "Save failed: " + msg.err.Error()
		} else {
			m.status = "Config saved"
			m.addLog("[config] saved")
		}
		return m, nil

	case runStartedMsg:
		return m, pumpProgress(msg.progCh, msg.doneCh)

	case progressMsg:
		ev := msg.event
		m.status = ev.Stage + ": " + ev.Message
		if ev.Progress != nil {
			m.progress = ev.Progress
		} else {
			m.addLog("[" + ev.Stage + "] " + ev.Message)
		}
		// Continue pumping
		return m, pumpProgress(msg.progCh, msg.doneCh)

	case runDoneMsg:
		m.running = false
		m.taskController = nil
		if msg.err != nil {
			m.status = "Task failed: " + msg.err.Error()
			m.addLog("[error] " + msg.err.Error())
			m.screen = screenProvider
		} else {
			m.lastResult = &msg.result
			m.status = "Completed: " + config.TaskLabel(msg.result.TaskType)
			m.addLog("[done] " + config.TaskLabel(msg.result.TaskType))
			m.screen = screenDone
		}
		return m, nil
	}

	return m, nil
}

func (m *model) addLog(s string) {
	m.logs = append(m.logs, s)
	if len(m.logs) > 50 {
		m.logs = m.logs[len(m.logs)-50:]
	}
}

func (m *model) resetDates() {
	today := config.BuildSnapshotID(time.Now())
	m.cfg.PyPI.MetadataSync.SnapshotDate = today
	if m.cfg.PyPI.ArtifactDownload.MetadataDate == "" {
		m.cfg.PyPI.ArtifactDownload.MetadataDate = today
	}
	m.cfg.PyPI.ArtifactDownload.OutputDate = today
	if m.cfg.PyPI.IncrementalDownload.OldMetadataDate == "" {
		m.cfg.PyPI.IncrementalDownload.OldMetadataDate = today
	}
	if m.cfg.PyPI.IncrementalDownload.NewMetadataDate == "" {
		m.cfg.PyPI.IncrementalDownload.NewMetadataDate = today
	}
	m.cfg.PyPI.IncrementalDownload.OutputDate = today
}

// ── Key dispatch ───────────────────────────────────────────────────────────

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	keyType := msg.Type

	// Global: Ctrl-C always quits
	if keyType == tea.KeyCtrlC {
		if m.running && m.taskController != nil {
			m.taskController.Cancel()
		}
		return m, tea.Quit
	}

	// Editing mode: delegate to text input
	if m.editingField != "" {
		switch {
		case keyType == tea.KeyEscape:
			m.editingField = ""
			m.textInput.SetValue("")
			m.textInput.Blur()
			m.status = "Edit cancelled"
			return m, nil

		case keyType == tea.KeyEnter:
			val := m.textInput.Value()
			f := m.editingField
			m.editingField = ""
			m.textInput.SetValue("")
			m.textInput.Blur()
			if strings.HasPrefix(f, "base:") {
				m.cfg = updateBaseField(m.cfg, strings.TrimPrefix(f, "base:"), val)
			} else if strings.HasPrefix(f, "task:") {
				m.cfg = updateTaskField(m.cfg, m.cfg.SelectedTask, strings.TrimPrefix(f, "task:"), val)
			}
			m.status = "Updated"
			return m, nil

		default:
			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(msg)
			return m, cmd
		}
	}

	// Running mode: p/c/q only
	if m.running {
		switch key {
		case "p":
			if m.taskController != nil {
				if m.taskController.IsPaused() {
					m.taskController.Resume()
					m.status = "Resumed"
				} else {
					m.taskController.Pause()
					m.status = "Paused"
				}
			}
		case "c":
			if m.taskController != nil {
				m.taskController.Cancel()
				m.status = "Cancelling..."
			}
		case "q", "ctrl+c":
			if m.taskController != nil {
				m.taskController.Cancel()
			}
			return m, tea.Quit
		}
		return m, nil
	}

	// Global quit (not running)
	if key == "q" {
		return m, tea.Quit
	}

	// Dispatch by screen
	switch m.screen {
	case screenProvider:
		return m.handleProvider(key, keyType)
	case screenTask:
		return m.handleTask(key, keyType)
	case screenConfig:
		return m.handleConfig(key, keyType)
	case screenConfirm:
		return m.handleConfirm(key, keyType)
	case screenDone:
		return m.handleDone(key, keyType)
	}

	return m, nil
}

// ── Screen: Provider ───────────────────────────────────────────────────────

func (m model) handleProvider(key string, keyType tea.KeyType) (tea.Model, tea.Cmd) {
	if keyType == tea.KeyEnter || key == "enter" || key == "n" {
		m.screen = screenTask
		m.status = "Provider: PyPI"
	}
	return m, nil
}

// ── Screen: Task ───────────────────────────────────────────────────────────

func (m model) handleTask(key string, keyType tea.KeyType) (tea.Model, tea.Cmd) {
	switch {
	case key == "up" || key == "k":
		m.taskIdx = (m.taskIdx - 1 + len(taskDefs)) % len(taskDefs)
	case key == "down" || key == "j":
		m.taskIdx = (m.taskIdx + 1) % len(taskDefs)
	case keyType == tea.KeyEnter || key == "enter":
		m.cfg.SelectedTask = taskDefs[m.taskIdx].id
		// Default to Task Settings section if the task has fields
		if len(taskDefs[m.taskIdx].fields) > 0 {
			m.configSection = configTask
		} else {
			m.configSection = configBase
		}
		m.baseFieldIdx = 0
		m.taskFieldIdx = 0
		m.screen = screenConfig
		m.status = "Task: " + taskDefs[m.taskIdx].label
	case key == "b" || key == "esc" || keyType == tea.KeyEscape:
		m.screen = screenProvider
		m.status = "Back to provider"
	}
	return m, nil
}

// ── Screen: Config ─────────────────────────────────────────────────────────

func (m model) handleConfig(key string, keyType tea.KeyType) (tea.Model, tea.Cmd) {
	switch {
	// Navigation
	case key == "b" || key == "esc" || keyType == tea.KeyEscape:
		m.screen = screenTask
		m.status = "Back to task selection"
		return m, nil

	case key == "tab":
		if m.configSection == configBase {
			m.configSection = configTask
		} else {
			m.configSection = configBase
		}

	case key == "h":
		m.configSection = configBase
	case key == "l":
		m.configSection = configTask

	// Up/down
	case key == "up" || key == "k":
		if m.configSection == configBase {
			m.baseFieldIdx = (m.baseFieldIdx - 1 + len(baseFields)) % len(baseFields)
		} else {
			f := taskDefs[m.taskIdx].fields
			if len(f) > 0 {
				m.taskFieldIdx = (m.taskFieldIdx - 1 + len(f)) % len(f)
			}
		}
	case key == "down" || key == "j":
		if m.configSection == configBase {
			m.baseFieldIdx = (m.baseFieldIdx + 1) % len(baseFields)
		} else {
			f := taskDefs[m.taskIdx].fields
			if len(f) > 0 {
				m.taskFieldIdx = (m.taskFieldIdx + 1) % len(f)
			}
		}

	// Edit
	case keyType == tea.KeyEnter || key == "enter" || key == "e":
		if m.configSection == configBase {
			return m.startBaseEdit()
		}
		return m.startTaskEdit()

	// Actions
	case key == "c":
		m.screen = screenConfirm
		m.status = "Review configuration"
		// Normalize and save on confirm transition
		cfg := config.NormalizeConfig(m.cfg)
		m.cfg = cfg
		return m, saveConfigCmd(cfg)
	case key == "s":
		m.saving = true
		cfg := config.NormalizeConfig(m.cfg)
		m.cfg = cfg
		return m, saveConfigCmd(cfg)
	}

	return m, nil
}

func (m model) startBaseEdit() (tea.Model, tea.Cmd) {
	f := baseFields[m.baseFieldIdx]
	m.editingField = "base:" + f.key
	m.textInput.SetValue(getBaseFieldValue(m.cfg, f.key))
	m.textInput.Focus()
	return m, textinput.Blink
}

func (m model) startTaskEdit() (tea.Model, tea.Cmd) {
	fields := taskDefs[m.taskIdx].fields
	if len(fields) == 0 {
		return m, nil
	}
	f := fields[m.taskFieldIdx]
	m.editingField = "task:" + f.key
	m.textInput.SetValue(getTaskFieldValue(m.cfg, m.cfg.SelectedTask, f.key))
	m.textInput.Focus()
	return m, textinput.Blink
}

func saveConfigCmd(cfg types.AppConfig) tea.Cmd {
	return func() tea.Msg {
		return saveDoneMsg{err: config.SaveConfig(cfg, "")}
	}
}

// ── Screen: Confirm ────────────────────────────────────────────────────────

func (m model) handleConfirm(key string, keyType tea.KeyType) (tea.Model, tea.Cmd) {
	switch {
	case key == "b" || key == "esc" || keyType == tea.KeyEscape:
		m.screen = screenConfig
		m.status = "Back to config"

	case keyType == tea.KeyEnter || key == "enter" || key == "r":
		m.running = true
		m.lastResult = nil
		m.progress = nil
		m.screen = screenRunning
		m.status = "Starting..."
		ctrl := taskctrl.New()
		m.taskController = ctrl
		// Force metadata-sync snapshot to today
		if m.cfg.SelectedTask == types.PypiTaskMetadataSync {
			m.cfg.PyPI.MetadataSync.SnapshotDate = config.BuildSnapshotID(time.Now())
		}
		m.addLog("[run] " + string(m.cfg.SelectedTask))
		cfg := config.NormalizeConfig(m.cfg)
		m.cfg = cfg
		return m, runTaskCmd(cfg, ctrl)

	case key == "s":
		m.saving = true
		cfg := config.NormalizeConfig(m.cfg)
		m.cfg = cfg
		return m, saveConfigCmd(cfg)
	}
	return m, nil
}

func runTaskCmd(cfg types.AppConfig, ctrl *taskctrl.Controller) tea.Cmd {
	return func() tea.Msg {
		progCh := make(chan runner.SyncEvent, 200)
		doneCh := make(chan runDoneMsg, 1)

		go func() {
			defer close(progCh)
			if err := config.SaveConfig(cfg, ""); err != nil {
				doneCh <- runDoneMsg{err: fmt.Errorf("save config: %w", err)}
				return
			}
			result, err := runner.RunSync(runner.RunSyncOptions{
				Config:         cfg,
				TaskController: ctrl,
				OnEvent: func(ev runner.SyncEvent) {
					select {
					case progCh <- ev:
					default: // drop if buffer full
					}
				},
			})
			doneCh <- runDoneMsg{result: result, err: err}
		}()

		return runStartedMsg{progCh: progCh, doneCh: doneCh}
	}
}

// pumpProgress reads one progress event and returns a Cmd to read the next.
// When progCh is exhausted, it collects the final result from doneCh.
func pumpProgress(progCh <-chan runner.SyncEvent, doneCh <-chan runDoneMsg) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-progCh
		if ok {
			return progressMsg{event: ev, progCh: progCh, doneCh: doneCh}
		}
		// progCh closed – task finished, read result
		return <-doneCh
	}
}

// ── Screen: Done ───────────────────────────────────────────────────────────

func (m model) handleDone(key string, keyType tea.KeyType) (tea.Model, tea.Cmd) {
	m.screen = screenProvider
	m.lastResult = nil
	m.progress = nil
	return m, nil
}

// ── View ───────────────────────────────────────────────────────────────────

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	w := m.width
	if w < 40 {
		w = 80
	}
	contentW := w - 4

	// ── Header ──
	steps := []struct {
		label  string
		active bool
	}{
		{"① Provider", m.screen == screenProvider},
		{"② Task", m.screen == screenTask},
		{"③ Config", m.screen == screenConfig},
		{"④ Run", m.screen == screenConfirm || m.screen == screenRunning || m.screen == screenDone},
	}
	var stepParts []string
	for _, s := range steps {
		if s.active {
			stepParts = append(stepParts, sGreen.Render(s.label))
		} else {
			stepParts = append(stepParts, sDim.Render(s.label))
		}
	}
	stepLine := strings.Join(stepParts, sDim.Render("  →  "))

	header := lipgloss.JoinVertical(lipgloss.Left,
		sTitle.Render("mirror-sync"),
		stepLine,
	)
	header = sBorder.Width(contentW).Render(header)

	// ── Body ──
	var body string
	switch m.screen {
	case screenProvider:
		body = m.viewProvider(contentW)
	case screenTask:
		body = m.viewTask(contentW)
	case screenConfig:
		body = m.viewConfig(contentW)
	case screenConfirm:
		body = m.viewConfirm(contentW)
	case screenRunning:
		body = m.viewRunning(contentW)
	case screenDone:
		body = m.viewDone(contentW)
	}

	// ── Status bar ──
	statusLine := sYellow.Render("● ") + m.status
	if m.loading {
		statusLine = sDim.Render("◌ Loading...")
	}
	statusBar := sBox.Width(contentW).Render(statusLine)

	// ── Log tail ──
	var logLines string
	show := m.logs
	if len(show) > 4 {
		show = show[len(show)-4:]
	}
	for _, l := range show {
		logLines += sDim.Render("  "+l) + "\n"
	}

	// ── Help ──
	help := m.helpText()
	helpBar := sDim.Render(help)

	// ── Assembly ──
	footer := lipgloss.JoinVertical(lipgloss.Left, statusBar, logLines, helpBar)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m model) helpText() string {
	if m.running {
		return "[p] Pause/Resume  [c] Cancel  [Ctrl+C] Quit"
	}
	switch m.screen {
	case screenProvider:
		return "[Enter] Next  [q] Quit"
	case screenTask:
		return "[↑/↓] Select  [Enter] Next  [b] Back  [q] Quit"
	case screenConfig:
		return "[↑/↓/Tab] Navigate  [Enter/e] Edit  [c] Confirm  [s] Save  [b] Back  [q] Quit"
	case screenConfirm:
		return "[Enter/r] Run  [s] Save  [b] Back  [q] Quit"
	case screenDone:
		return "[Any key] Continue  [q] Quit"
	}
	return "[q] Quit"
}

// ── View: Provider ─────────────────────────────────────────────────────────

func (m model) viewProvider(w int) string {
	lines := []string{
		sSection.Render("Select Provider"),
		"",
		sHighlight.Render("  PyPI"),
		sDim.Render("    Python Package Index — 同步 simple 元数据与 packages 包文件。"),
		"",
		sDim.Render("  Only PyPI is supported for now."),
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// ── View: Task ─────────────────────────────────────────────────────────────

func (m model) viewTask(w int) string {
	var lines []string
	lines = append(lines, sSection.Render("Select Task"), "")

	for i, td := range taskDefs {
		prefix := "  "
		if i == m.taskIdx {
			prefix = sHighlight.Render("▸ ")
		}
		lines = append(lines,
			prefix+sBold.Render(td.label),
			sDim.Render("    "+td.description),
			"",
		)
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// ── View: Config ───────────────────────────────────────────────────────────

func (m model) viewConfig(w int) string {
	var lines []string
	lines = append(lines, sSection.Render("Configuration"), "")

	// Base section
	secLabel := "  Base Settings"
	if m.configSection == configBase {
		secLabel = sHighlight.Render("▸ Base Settings")
	}
	lines = append(lines, sCyan.Render(secLabel))

	for i, f := range baseFields {
		sel := m.configSection == configBase && i == m.baseFieldIdx && m.editingField == ""
		edit := m.editingField == "base:"+f.key
		val := getBaseFieldValue(m.cfg, f.key)
		lines = append(lines, m.renderField(f.label, val, sel, edit))
	}

	lines = append(lines, "")

	// Task section — only show when there are task-specific fields
	if len(taskDefs[m.taskIdx].fields) > 0 {
		secLabel2 := "  Task Settings"
		if m.configSection == configTask {
			secLabel2 = sHighlight.Render("▸ Task Settings")
		}
		lines = append(lines, sCyan.Render(secLabel2))

		for i, f := range taskDefs[m.taskIdx].fields {
			sel := m.configSection == configTask && i == m.taskFieldIdx && m.editingField == ""
			edit := m.editingField == "task:"+f.key
			val := getTaskFieldValue(m.cfg, m.cfg.SelectedTask, f.key)
			lines = append(lines, m.renderField(f.label, val, sel, edit))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m model) renderField(label, value string, selected, editing bool) string {
	indent := "     "
	prefix := indent + "  "
	suffix := ""

	if selected {
		prefix = sGreen.Render(indent + "▸ ")
		suffix = sGreen.Render(value)
	} else if editing {
		prefix = sCyan.Render(indent + "✎ ")
		suffix = m.textInput.View()
	} else {
		suffix = value
	}

	return fmt.Sprintf("%s%-16s %s", prefix, label+":", suffix)
}

// ── View: Confirm ──────────────────────────────────────────────────────────

func (m model) viewConfirm(w int) string {
	cfg := m.cfg
	task := taskDefs[m.taskIdx]

	var items []string
	items = append(items,
		fmt.Sprintf("Provider:    PyPI"),
		fmt.Sprintf("Task:        %s", task.label),
		fmt.Sprintf("Simple URL:  %s", cfg.Base.SimpleURL),
		fmt.Sprintf("Concurrency: %d", cfg.Base.Concurrency),
		fmt.Sprintf("Retry:       %d", cfg.Base.Retry),
		fmt.Sprintf("Timeout:     %dms", cfg.Base.TimeoutMs),
	)

	// Task-specific params
	switch cfg.SelectedTask {
	case types.PypiTaskMetadataSync:
		// Always use today — no config needed
		items = append(items, fmt.Sprintf("Snapshot:    %s", config.BuildSnapshotID(time.Now())))
	case types.PypiTaskArtifactDownload:
		items = append(items,
			fmt.Sprintf("Source Date: %s", cfg.PyPI.ArtifactDownload.MetadataDate),
			fmt.Sprintf("Output Date: %s", cfg.PyPI.ArtifactDownload.OutputDate),
		)
	case types.PypiTaskIncrementalDownload:
		items = append(items,
			fmt.Sprintf("Old Date:    %s", cfg.PyPI.IncrementalDownload.OldMetadataDate),
			fmt.Sprintf("New Date:    %s", cfg.PyPI.IncrementalDownload.NewMetadataDate),
			fmt.Sprintf("Output Date: %s", cfg.PyPI.IncrementalDownload.OutputDate),
		)
	}

	body := strings.Join(items, "\n")
	prompt := sPrompt.Render("\n  Press Enter to start")

	content := lipgloss.JoinVertical(lipgloss.Left,
		sSection.Render("Ready to Run"),
		"",
		body,
		prompt,
	)
	return sGreenBox.Width(w).Render(content)
}

// ── View: Running ──────────────────────────────────────────────────────────

func (m model) viewRunning(w int) string {
	var lines []string
	lines = append(lines, sSection.Render("Running"), "")

	// Pause banner
	if m.taskController != nil && m.taskController.IsPaused() {
		lines = append(lines, sYellow.Render("  ⏸  PAUSED — press [p] to resume"), "")
	}

	// Progress bar
	if m.progress != nil && m.progress.Total > 0 {
		pct := 0
		if m.progress.Total > 0 {
			pct = m.progress.Current * 100 / m.progress.Total
		}
		if pct > 100 {
			pct = 100
		}
		barW := w - 8
		if barW > 40 {
			barW = 40
		}
		bar := renderBar(pct, barW)
		barColor := sCyan
		if m.taskController != nil && m.taskController.IsPaused() {
			barColor = sYellow
		}

		lines = append(lines,
			barColor.Render(fmt.Sprintf("  %s", bar)),
			fmt.Sprintf("  %s / %s  (%d%%)",
				boldNum(m.progress.Current), boldNum(m.progress.Total), pct),
		)

		if m.progress.Failed > 0 {
			lines = append(lines, sRed.Render(fmt.Sprintf("  ✗ Failed: %d", m.progress.Failed)))
		}
	} else {
		lines = append(lines, sDim.Render("  Initializing..."))
	}

	// Active items
	if m.progress != nil && len(m.progress.Active) > 0 {
		lines = append(lines, "")
		active := m.progress.Active
		show := len(active)
		if show > 0 {
			lines = append(lines, sDim.Render(fmt.Sprintf("  Active (%d)", show)))
			limit := 3
			if limit > show {
				limit = show
			}
			// Show items, truncating long filenames
			for _, a := range active[:limit] {
				display := a
				if len(display) > w-8 {
					display = display[:w-11] + "..."
				}
				lines = append(lines, sDimCyan.Render("    ◌ "+display))
			}
			if show > limit {
				lines = append(lines, sDim.Render(fmt.Sprintf("    ... +%d more", show-limit)))
			}
		}
	}

	// Footer hint
	if m.taskController != nil {
		state := m.taskController.GetState()
		switch state {
		case "paused":
			lines = append(lines, "", sYellow.Render("  Task is paused. Press [p] to resume, [c] to cancel."))
		case "cancelled":
			lines = append(lines, "", sYellow.Render("  Cancelling... waiting for workers to stop."))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func renderBar(pct, width int) string {
	if width < 5 {
		width = 5
	}
	filled := pct * width / 100
	if filled > width {
		filled = width
	}
	empty := width - filled
	if empty < 0 {
		empty = 0
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", empty)
}

func boldNum(n int) string {
	return sBold.Render(fmt.Sprintf("%d", n))
}

// ── View: Done ─────────────────────────────────────────────────────────────

func (m model) viewDone(w int) string {
	var lines []string
	lines = append(lines, sGreen.Render("✓ Task Complete"), "")

	if m.lastResult != nil {
		r := m.lastResult
		lines = append(lines, fmt.Sprintf("  Provider: %s", r.Provider))
		lines = append(lines, fmt.Sprintf("  Task:     %s", config.TaskLabel(r.TaskType)))
		if r.SnapshotID != nil {
			lines = append(lines, fmt.Sprintf("  Snapshot: %s", *r.SnapshotID))
		}
		if r.PackageCount != nil {
			lines = append(lines, fmt.Sprintf("  Packages: %d", *r.PackageCount))
		}
		if r.Plan != nil {
			lines = append(lines, fmt.Sprintf("  Planned:  %d entries", len(r.Plan.Entries)))
		}
		if r.DownloadSummary != nil {
			ds := r.DownloadSummary
			lines = append(lines,
				fmt.Sprintf("  Downloaded: %d / %d", ds.Downloaded, ds.Attempted),
				fmt.Sprintf("  Failed:     %d", len(ds.Failed)),
			)
		}
		if r.OutputRoot != nil {
			lines = append(lines, fmt.Sprintf("  Output:  %s", *r.OutputRoot))
		}
	}

	lines = append(lines, "", sDim.Render("  Press any key to continue."))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// ── Field helpers ──────────────────────────────────────────────────────────

func getBaseFieldValue(cfg types.AppConfig, key string) string {
	switch key {
	case "profileName":
		return cfg.Base.ProfileName
	case "simpleUrl":
		return cfg.Base.SimpleURL
	case "metadataRoot":
		return cfg.Base.MetadataRoot
	case "mirrorRoot":
		return cfg.Base.MirrorRoot
	case "concurrency":
		return fmt.Sprintf("%d", cfg.Base.Concurrency)
	case "retry":
		return fmt.Sprintf("%d", cfg.Base.Retry)
	case "timeoutMs":
		return fmt.Sprintf("%d", cfg.Base.TimeoutMs)
	}
	return ""
}

func updateBaseField(cfg types.AppConfig, key, value string) types.AppConfig {
	switch key {
	case "profileName":
		cfg.Base.ProfileName = value
	case "simpleUrl":
		cfg.Base.SimpleURL = value
	case "metadataRoot":
		cfg.Base.MetadataRoot = value
	case "mirrorRoot":
		cfg.Base.MirrorRoot = value
	case "concurrency", "retry", "timeoutMs":
		var num int
		if _, err := fmt.Sscanf(value, "%d", &num); err == nil {
			switch key {
			case "concurrency":
				cfg.Base.Concurrency = num
			case "retry":
				cfg.Base.Retry = num
			case "timeoutMs":
				cfg.Base.TimeoutMs = num
			}
		}
	}
	return cfg
}

func getTaskFieldValue(cfg types.AppConfig, taskType types.PypiTaskType, key string) string {
	switch taskType {
	case types.PypiTaskMetadataSync:
		if key == "snapshotDate" {
			return cfg.PyPI.MetadataSync.SnapshotDate
		}
	case types.PypiTaskArtifactDownload:
		switch key {
		case "metadataDate":
			return cfg.PyPI.ArtifactDownload.MetadataDate
		case "outputDate":
			return cfg.PyPI.ArtifactDownload.OutputDate
		}
	case types.PypiTaskIncrementalDownload:
		switch key {
		case "oldMetadataDate":
			return cfg.PyPI.IncrementalDownload.OldMetadataDate
		case "newMetadataDate":
			return cfg.PyPI.IncrementalDownload.NewMetadataDate
		case "outputDate":
			return cfg.PyPI.IncrementalDownload.OutputDate
		}
	}
	return ""
}

func updateTaskField(cfg types.AppConfig, taskType types.PypiTaskType, key, value string) types.AppConfig {
	switch taskType {
	case types.PypiTaskMetadataSync:
		if key == "snapshotDate" {
			cfg.PyPI.MetadataSync.SnapshotDate = value
		}
	case types.PypiTaskArtifactDownload:
		switch key {
		case "metadataDate":
			cfg.PyPI.ArtifactDownload.MetadataDate = value
		case "outputDate":
			cfg.PyPI.ArtifactDownload.OutputDate = value
		}
	case types.PypiTaskIncrementalDownload:
		switch key {
		case "oldMetadataDate":
			cfg.PyPI.IncrementalDownload.OldMetadataDate = value
		case "newMetadataDate":
			cfg.PyPI.IncrementalDownload.NewMetadataDate = value
		case "outputDate":
			cfg.PyPI.IncrementalDownload.OutputDate = value
		}
	}
	return cfg
}
