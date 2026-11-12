package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	// The tabbed shell needs room for nine tabs and a body that still reads.
	minTermWidth  = 100
	minTermHeight = 30
	// tab bar (3) + body border (2) + key bar (3)
	chromeHeight = 8
)

// tabNames are the nine workspaces. Index 0 is the pLaya home tab.
var tabNames = [...]string{
	"pLaya", "State", "Questions", "Batch", "Bench",
	"Compare", "Snippets", "History", "Server",
}

func tabForScreen(s screen) int {
	switch s {
	case screenState, screenWindow:
		return 1
	case screenForge:
		return 2
	case screenBatch:
		return 3
	case screenBench:
		return 4
	case screenAB:
		return 5
	case screenSnippets:
		return 6
	case screenHistory:
		return 7
	case screenLaunch:
		return 8
	}
	return 0
}

func screenForTab(t int) screen {
	switch t {
	case 1:
		return screenState
	case 2:
		return screenForge
	case 3:
		return screenBatch
	case 4:
		return screenBench
	case 5:
		return screenAB
	case 6:
		return screenSnippets
	case 7:
		return screenHistory
	case 8:
		return screenLaunch
	}
	return screenBrowse
}

// tabRunning reports whether a workspace has a background job worth marking.
func (m Model) tabRunning(t int) bool {
	switch t {
	case 0:
		return m.running && m.runTab == 0
	case 1:
		return (m.windowed != nil && m.windowed.running) || (m.running && m.runTab == 1)
	case 3:
		return m.batch != nil && m.batch.running
	case 4:
		return m.bench != nil && m.bench.running
	case 5:
		return m.ab != nil && m.ab.running
	case 7:
		return m.running && m.runTab == 7
	case 8:
		return m.launch != nil && m.launch.starting
	}
	return false
}

// bodyW is the width available inside the body border and padding.
func (m Model) bodyW() int {
	w := m.width - 4
	if w < 20 {
		w = 20
	}
	return w
}

// bodyH is the height available inside the body border.
func (m Model) bodyH() int {
	h := m.height - chromeHeight
	if h < 3 {
		h = 3
	}
	return h
}

// padVisible pads s to n visible columns. It never truncates; callers keep
// their content narrow enough for the frame.
func padVisible(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

// boxed draws a single-line rounded border around content. width is the total
// width including the border; height is the number of interior lines.
func boxed(content string, width, height int, color lipgloss.Color) string {
	if width < 4 {
		width = 4
	}
	if height < 1 {
		height = 1
	}
	inner := width - 2
	contentW := inner - 2 // one column of padding on each side
	if contentW < 1 {
		contentW = 1
	}
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	out := make([]string, height)
	for i := 0; i < height; i++ {
		var line string
		if i < len(lines) {
			line = lines[i]
		}
		line = padVisible(line, contentW)
		out[i] = " " + line + " "
		if w := lipgloss.Width(out[i]); w < inner {
			out[i] += strings.Repeat(" ", inner-w)
		}
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color).
		Render(strings.Join(out, "\n"))
}

func (m Model) viewTabBar() string {
	active := tabForScreen(m.mode)
	labels := make([]string, len(tabNames))
	for i, name := range tabNames {
		label := "[" + name + "]"
		if i > 0 {
			label = fmt.Sprintf("[%d]%s", i, name)
		}
		if m.tabRunning(i) {
			label += "*"
		}
		if i == active {
			labels[i] = tabActiveStyle.Render(label)
		} else {
			labels[i] = tabInactiveStyle.Render(label)
		}
	}
	left := strings.Join(labels, " ")
	contentW := m.width - 4
	if contentW < 1 {
		contentW = 1
	}
	right := ""
	if m.width >= 120 && m.client != nil {
		right = dimStyle.Render(m.client.BaseURL)
	}
	pad := contentW - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		right = ""
		pad = contentW - lipgloss.Width(left)
	}
	if pad < 0 {
		pad = 0
	}
	content := left + strings.Repeat(" ", pad) + right
	return boxed(content, m.width, 1, colAccent)
}

func runningSuffix(m Model) string {
	if m.activeJobRunning() {
		return "esc cancel | running... | "
	}
	return ""
}

func (m Model) viewKeyBar() string {
	text := runningSuffix(m) + m.keyBarText()
	text = truncate(text, m.width-4)
	return boxed(dimStyle.Render(text), m.width, 1, colDim)
}

func (m Model) keyBarText() string {
	switch m.mode {
	case screenState:
		return "tab fields | ctrl+r raw | ctrl+w windows | f model | R reset | esc back | ctrl+h help | q quit"
	case screenForge:
		return "j/k move | n new | e edit | d delete | p preview | r raw | R reset | esc back | ctrl+h help | q quit"
	case screenBatch:
		return "j/k rows | h/l columns | enter detail | r run | R rerun | F filter | f model | esc back | ctrl+h help | q quit"
	case screenBench:
		return "enter run | f model | [ ] runs | w warmup | R reload | esc back | ctrl+h help | q quit"
	case screenAB:
		return "tab target | f model | u url | R reset url | enter run | esc back | ctrl+h help | q quit"
	case screenSnippets:
		return "j/k move | enter load | s save | d delete | v raw | R refresh | esc back | ctrl+h help | q quit"
	case screenHistory:
		return "j/k move | enter replay | v raw | R reload | esc back | ctrl+h help | q quit"
	case screenLaunch:
		return "j/k field | e edit | s start | x stop | a attach | w warm | R health | esc back | ctrl+h help | q quit"
	case screenWindow:
		return "enter run | [ ] size | o overlap | j/k select | v raw | R re-split | esc back | ctrl+h help | q quit"
	}
	return "j/k select | enter run | v raw | f model | R reload | ctrl+r reset | ctrl+h help | q quit"
}

// viewTooSmall is the centered message shown before the tabbed shell fits.
func viewTooSmall(width, height int) string {
	msg := []string{
		"terminal too small",
		fmt.Sprintf("need at least %dx%d", minTermWidth, minTermHeight),
		fmt.Sprintf("current %dx%d", width, height),
	}
	if height <= 0 {
		return ""
	}
	top := (height - len(msg)) / 2
	if top < 0 {
		top = 0
	}
	lines := make([]string, 0, height)
	for i := 0; i < top; i++ {
		lines = append(lines, "")
	}
	for _, line := range msg {
		pad := (width - len([]rune(line))) / 2
		if pad < 0 {
			pad = 0
		}
		lines = append(lines, strings.Repeat(" ", pad)+dimStyle.Render(line))
		if len(lines) >= height {
			break
		}
	}
	return strings.Join(lines, "\n")
}

// Toast messages are transient; long-running progress stays in the tab body.
type toast struct {
	text  string
	err   bool
	until time.Time
}

func (m *Model) notify(text string) {
	if text == "" {
		return
	}
	m.toasts = append(m.toasts, toast{text: text, until: time.Now().Add(4 * time.Second)})
	if len(m.toasts) > 3 {
		m.toasts = m.toasts[len(m.toasts)-3:]
	}
}

func (m *Model) notifyErr(text string) {
	if text == "" {
		return
	}
	m.toasts = append(m.toasts, toast{text: text, err: true, until: time.Now().Add(8 * time.Second)})
	if len(m.toasts) > 3 {
		m.toasts = m.toasts[len(m.toasts)-3:]
	}
}

// pruneToasts drops expired messages and, on the next key, error messages.
func (m *Model) pruneToasts(nextKey bool) {
	now := time.Now()
	out := m.toasts[:0]
	for _, t := range m.toasts {
		if now.After(t.until) {
			continue
		}
		if nextKey && t.err {
			continue
		}
		out = append(out, t)
	}
	m.toasts = out
}

func (m Model) toastLines(width int) []string {
	now := time.Now()
	var lines []string
	for _, t := range m.toasts {
		if !now.Before(t.until) {
			continue
		}
		style := toastStyle
		if t.err {
			style = toastErrStyle
		}
		text := truncate(oneLine(t.text), max(1, width-4))
		line := style.Render(text)
		pad := width - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
		}
		lines = append(lines, strings.Repeat(" ", pad)+line)
	}
	return lines
}

// helpSections is the single global help document. It is deliberately the same
// from every tab.
type helpSection struct {
	title string
	rows  [][2]string
}

var helpSections = []helpSection{
	{"Tabs", [][2]string{
		{"alt+1..8", "open State, Questions, Batch, Bench, Compare, Snippets, History, Server"},
		{"1..8", "same, when not typing or in a modal"},
		{"alt+0 / 0", "return to the pLaya home tab"},
		{"tab / shift+tab", "cycle tabs where the screen does not bind tab"},
	}},
	{"Everywhere", [][2]string{
		{"ctrl+h", "open or close this help"},
		{"ctrl+c", "quit, even while typing"},
		{"q", "quit when not typing and not confirming"},
		{"esc", "close a modal, cancel a job, leave text, then go home"},
		{"f", "cycle the model family for the current context"},
		{"R", "reload or reset the current tab"},
	}},
	{"pLaya", [][2]string{
		{"j/k", "select a preset"},
		{"enter", "run the selected preset"},
		{"v", "toggle the raw response"},
		{"R", "reload presets and health"},
		{"ctrl+r", "reset the draft to the server copy"},
	}},
	{"State", [][2]string{
		{"tab", "switch between text and other fields"},
		{"ctrl+r", "edit the whole state as JSON"},
		{"ctrl+w", "open Windowed Run"},
		{"R", "revert the state to the server copy"},
	}},
	{"Questions", [][2]string{
		{"j/k", "select a question"},
		{"n / e / d", "new / edit / delete"},
		{"J / K", "move a question down / up"},
		{"p / r", "preview payload / edit raw JSON"},
		{"ctrl+s / ctrl+o", "save / load a question set"},
		{"R", "revert questions to the server copy"},
	}},
	{"Batch", [][2]string{
		{"j/k, h/l", "move rows, scroll question columns"},
		{"enter", "open row detail"},
		{"r / R", "run or resume / rerun from scratch"},
		{"F", "cycle the filter"},
		{"s / S / t", "sort column / direction / noul threshold"},
		{"ctrl+o / x", "open a file / export results"},
	}},
	{"Bench", [][2]string{
		{"enter", "start the benchmark"},
		{"[ / ]", "decrease / increase runs"},
		{"w", "cycle warm-up count"},
		{"R", "reload saved results"},
	}},
	{"Compare", [][2]string{
		{"tab", "switch target A or B"},
		{"f", "cycle the selected target's model"},
		{"u", "edit the selected target's URL"},
		{"R", "reset the selected URL to the primary server"},
		{"enter", "run both targets"},
	}},
	{"Snippets", [][2]string{
		{"j/k", "select a snippet"},
		{"enter", "load it into the current draft"},
		{"s / d / y", "save / request delete / confirm delete"},
		{"v", "toggle the raw stored JSON"},
		{"R", "refresh from disk"},
	}},
	{"History", [][2]string{
		{"j/k", "select a run"},
		{"enter", "replay the exact request"},
		{"v", "toggle the stored raw response"},
		{"R", "reload from disk"},
	}},
	{"Server", [][2]string{
		{"j/k", "select a field"},
		{"e / enter", "edit the selected field"},
		{"s / x", "start / stop the spawned server"},
		{"a / w", "attach / warm every family"},
		{"R", "re-check health"},
	}},
	{"Windowed Run", [][2]string{
		{"enter", "run all windows"},
		{"[ / ]", "change window size"},
		{"o", "cycle overlap"},
		{"j/k, v", "select a window, toggle raw"},
		{"R", "re-split with the current state"},
	}},
}

func helpLines(width int) []string {
	var out []string
	out = append(out, titleStyle.Render("pLaya help")+"  "+dimStyle.Render("esc close | j/k scroll"))
	out = append(out, "")
	for _, section := range helpSections {
		out = append(out, titleStyle.Render(section.title))
		for _, row := range section.rows {
			line := fmt.Sprintf("  %-18s %s", row[0], row[1])
			out = append(out, dimStyle.Render(truncate(line, width)))
		}
		out = append(out, "")
	}
	return out
}

func (m Model) helpPage() int {
	page := m.height - 4
	if page < 1 {
		page = 1
	}
	return page
}

func (m Model) viewHelp() string {
	lines := helpLines(m.width - 4)
	page := m.helpPage()
	maxScroll := len(lines) - page
	if maxScroll < 0 {
		maxScroll = 0
	}
	off := clamp(m.helpScroll, 0, maxScroll)
	if off > len(lines) {
		off = len(lines)
	}
	end := min(len(lines), off+page)
	content := strings.Join(lines[off:end], "\n")
	height := m.height - 2
	if height < 1 {
		height = 1
	}
	return boxed(content, m.width, height, colAccent)
}

func (m Model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	page := m.helpPage()
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "ctrl+h":
		m.helpOpen = false
	case "up", "k":
		m.helpScroll--
	case "down", "j":
		m.helpScroll++
	case "pgup":
		m.helpScroll -= page
	case "pgdown":
		m.helpScroll += page
	case "g", "home":
		m.helpScroll = 0
	case "G", "end":
		m.helpScroll = len(helpLines(m.width - 4))
	}
	if m.helpScroll < 0 {
		m.helpScroll = 0
	}
	return m, nil
}

// activeJobRunning reports whether the current tab has a cancellable job. The
// key bar and tab marker use it to advertise progress that continues while the
// user is elsewhere.
func (m Model) activeJobRunning() bool {
	switch m.mode {
	case screenBrowse:
		return m.running && m.runTab == 0
	case screenHistory:
		return m.running && m.runTab == 7
	case screenWindow:
		return m.windowed != nil && m.windowed.running
	case screenBatch:
		return m.batch != nil && m.batch.running
	case screenBench:
		return m.bench != nil && m.bench.running
	case screenAB:
		return m.ab != nil && m.ab.running
	case screenLaunch:
		return m.launch != nil && m.launch.starting
	}
	return false
}

// typing reports whether the current tab has a focused text editor, input or
// form. Global single-letter keys must not steal those keystrokes.
func (m Model) typing() bool {
	switch m.mode {
	case screenState:
		e := m.stateEd
		if e == nil {
			return false
		}
		if e.form != nil {
			return true
		}
		return (e.sub == stateSubText || e.sub == stateSubRaw) && e.focused
	case screenForge:
		e := m.forgeEd
		if e == nil {
			return false
		}
		if e.form != nil {
			return true
		}
		return e.sub == forgeSubRaw || e.sub == forgeSubPreview
	case screenBatch:
		return m.batch != nil && m.batch.prompt
	case screenAB:
		return m.ab != nil && m.ab.editing
	case screenLaunch:
		return m.launch != nil && m.launch.editing
	case screenSnippets:
		return m.snippets != nil && m.snippets.prompt
	}
	return false
}

// inConfirmModal reports whether a destructive action is waiting for a y/n
// answer. q must not quit from there.
func (m Model) inConfirmModal() bool {
	return m.mode == screenSnippets && m.snippets != nil && m.snippets.pendingDelete != ""
}

// bindsTab reports whether the current tab owns Tab for its own navigation.
func (m Model) bindsTab() bool {
	return m.mode == screenState || m.mode == screenAB
}

func altTabKey(key string) (int, bool) {
	switch key {
	case "alt+0":
		return 0, true
	case "alt+1":
		return 1, true
	case "alt+2":
		return 2, true
	case "alt+3":
		return 3, true
	case "alt+4":
		return 4, true
	case "alt+5":
		return 5, true
	case "alt+6":
		return 6, true
	case "alt+7":
		return 7, true
	case "alt+8":
		return 8, true
	}
	return 0, false
}

func bareTabKey(key string) (int, bool) {
	switch key {
	case "0":
		return 0, true
	case "1":
		return 1, true
	case "2":
		return 2, true
	case "3":
		return 3, true
	case "4":
		return 4, true
	case "5":
		return 5, true
	case "6":
		return 6, true
	case "7":
		return 7, true
	case "8":
		return 8, true
	}
	return 0, false
}

// switchTab makes t the visible workspace. A workspace that has never been
// built, or whose draft changed since it was built, is recreated; otherwise the
// existing session (including a running job) is reused.
func (m Model) switchTab(t int) (tea.Model, tea.Cmd) {
	if t < 0 || t > 8 {
		return m, nil
	}
	target := screenForTab(t)
	if m.mode == target {
		return m, nil
	}
	d := m.currentDraft()
	switch t {
	case 0:
		m.mode = screenBrowse
	case 1:
		if d == nil {
			return m, nil
		}
		if m.stateEd == nil || m.stateEd.preset != d.preset {
			return m.openStateEditor()
		}
		m.mode = screenState
	case 2:
		if d == nil {
			return m, nil
		}
		if m.forgeEd == nil || m.forgeEd.preset != d.preset {
			return m.openForge()
		}
		m.mode = screenForge
	case 3:
		if m.batch == nil || (d != nil && m.batch.preset != d.preset) {
			return m.openBatch()
		}
		m.mode = screenBatch
	case 4:
		if m.bench == nil || (d != nil && m.bench.preset != d.preset) {
			return m.openBench()
		}
		m.mode = screenBench
	case 5:
		if m.ab == nil || (d != nil && m.ab.preset != d.preset) {
			return m.openAB()
		}
		m.mode = screenAB
	case 6:
		if m.snippets == nil {
			return m.openSnippets(screenBrowse)
		}
		m.mode = screenSnippets
	case 7:
		m.mode = screenHistory
	case 8:
		return m.openLaunch()
	}
	return m, nil
}

func (m Model) cycleTab(back bool) (tea.Model, tea.Cmd) {
	cur := tabForScreen(m.mode)
	next := cur + 1
	if back {
		next = cur - 1
	}
	if next > 8 {
		next = 0
	}
	if next < 0 {
		next = 8
	}
	return m.switchTab(next)
}

// cycleModelForTab applies f to the model context of the current tab.
func (m Model) cycleModelForTab() (tea.Model, tea.Cmd) {
	switch m.mode {
	case screenBatch:
		if m.batch != nil && !m.batch.running {
			m.batch.cycleModel(m.health)
		}
	case screenBench:
		if m.bench != nil && !m.bench.running {
			m.bench.cycleModel(m.health)
		}
	case screenAB:
		if m.ab != nil && !m.ab.running {
			m.ab.cycleModel(m.health)
		}
	case screenWindow:
		if m.windowed != nil && !m.windowed.running {
			m.windowed.cycleModel(m.health)
		}
	default:
		m.cycleModel()
	}
	return m, nil
}

// reloadTab is R: reload or reset the current tab's context.
func (m Model) reloadTab() (tea.Model, tea.Cmd) {
	switch m.mode {
	case screenBrowse:
		m.cancelRun()
		m.status, m.statusMsg, m.loadErr = statusConnecting, "", ""
		m.notice = ""
		return m, tea.Batch(m.healthCmd(), m.presetsCmd())
	case screenState:
		if d := m.currentDraft(); d != nil {
			if p, ok := m.presetByName(d.preset); ok {
				d.revertState(p)
				if m.stateEd != nil {
					m.stateEd.sync(d)
				}
				m.notify("state reverted to the server preset")
			}
		}
	case screenForge:
		if d := m.currentDraft(); d != nil {
			if p, ok := m.presetByName(d.preset); ok {
				d.revertQuestions(p)
				m.notify("questions reverted to the server preset")
			}
		}
	case screenBatch:
		return m, m.startBatch(true)
	case screenBench:
		if m.bench != nil {
			m.bench.reloadSaved(m.benchStore)
			m.bench.status = "saved results reloaded"
		}
	case screenAB:
		if m.ab != nil && !m.ab.running {
			m.ab.targets[m.ab.selected].URL = m.ab.primaryURL
			m.ab.status = abName(m.ab.selected) + " URL reset"
		}
	case screenSnippets:
		if m.snippets != nil {
			m.snippets.reload("")
			m.snippets.status = fmt.Sprintf("%d saved snippets", len(m.snippets.names))
		}
	case screenHistory:
		m.reloadHistory()
	case screenLaunch:
		if m.launch != nil {
			m.launch.status = "re-checking health…"
		}
		return m, m.healthCmd()
	case screenWindow:
		if m.windowed != nil && !m.windowed.running {
			if d := m.currentDraft(); d != nil {
				m.windowed.resplit(d)
				m.notify("windows re-split from the current state")
			}
		}
	}
	return m, nil
}

// handleEsc implements the layered escape: close the innermost modal or editor
// first, cancel a job next, leave a text focus after that, and go home last.
func (m Model) handleEsc() (tea.Model, tea.Cmd) {
	switch m.mode {
	case screenBrowse:
		if m.running && m.runTab == 0 && m.cancelRun() {
			m.notify("cancelled the in-flight request")
		}
		return m, nil
	case screenState:
		e := m.stateEd
		if e == nil {
			return m.switchTab(0)
		}
		if e.form != nil {
			e.form, e.err = nil, ""
			return m, nil
		}
		switch e.sub {
		case stateSubRaw:
			e.sub, e.err = stateSubText, ""
			e.focus(true)
			return m, nil
		case stateSubFields:
			return m.switchTab(0)
		}
		if e.focused {
			e.focus(false)
			return m, nil
		}
		return m.switchTab(0)
	case screenWindow:
		if m.cancelWindow() {
			return m, nil
		}
		return m.switchTab(1)
	case screenForge:
		e := m.forgeEd
		if e == nil {
			return m.switchTab(0)
		}
		if e.form != nil {
			e.form, e.formKind, e.err = nil, forgeFormNone, ""
			e.sub = forgeSubList
			return m, nil
		}
		if e.sub == forgeSubRaw || e.sub == forgeSubPreview {
			e.sub, e.err = forgeSubList, ""
			return m, nil
		}
		return m.switchTab(0)
	case screenBatch:
		e := m.batch
		if e == nil {
			return m.switchTab(0)
		}
		if e.prompt {
			e.prompt = false
			e.input.Blur()
			return m, nil
		}
		if e.detail {
			e.detail = false
			return m, nil
		}
		if m.cancelBatch() {
			return m, nil
		}
		return m.switchTab(0)
	case screenBench:
		if m.cancelBench() {
			return m, nil
		}
		return m.switchTab(0)
	case screenAB:
		e := m.ab
		if e == nil {
			return m.switchTab(0)
		}
		if e.editing {
			e.editing = false
			e.input.Blur()
			e.status = "URL unchanged"
			return m, nil
		}
		if m.cancelAB() {
			return m, nil
		}
		return m.switchTab(0)
	case screenLaunch:
		e := m.launch
		if e == nil {
			return m.switchTab(0)
		}
		if e.editing {
			e.editing = false
			e.input.Blur()
			e.status = "edit cancelled"
			return m, nil
		}
		if e.starting {
			wasStarting := e.starting
			e.seq++
			e.starting = false
			e.logSeq++
			if wasStarting {
				if m.launcher != nil && m.launcher.Running() {
					e.status = "wait cancelled; the child keeps running (a attach · x stop)"
				} else {
					e.status = "start wait cancelled"
				}
			}
			return m, nil
		}
		return m.switchTab(0)
	case screenSnippets:
		e := m.snippets
		if e == nil {
			return m.switchTab(0)
		}
		if e.prompt {
			e.prompt = false
			e.input.Blur()
			e.status = ""
			return m, nil
		}
		if e.pendingDelete != "" {
			e.pendingDelete = ""
			e.status = "delete cancelled"
			return m, nil
		}
		return m.switchTab(0)
	case screenHistory:
		if m.running && m.runTab == 7 && m.cancelRun() {
			m.notify("cancelled the in-flight replay")
			return m, nil
		}
		return m.switchTab(0)
	}
	return m, nil
}

// handleGlobalKey routes the keys that mean the same thing on every tab. It
// returns handled=false when the tab handler should see the key.
func (m Model) handleGlobalKey(msg tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	key := msg.String()

	if m.helpOpen {
		next, cmd := m.handleHelpKey(msg)
		return true, next, cmd
	}
	if key == "ctrl+c" {
		return true, m, tea.Quit
	}
	if key == "ctrl+h" {
		m.helpOpen = true
		m.helpScroll = 0
		return true, m, nil
	}

	if tab, ok := altTabKey(key); ok {
		next, cmd := m.switchTab(tab)
		return true, next, cmd
	}
	if tab, ok := bareTabKey(key); ok && !m.typing() && !m.inConfirmModal() {
		next, cmd := m.switchTab(tab)
		return true, next, cmd
	}

	if key == "tab" || key == "shift+tab" {
		if !m.bindsTab() && !m.typing() {
			next, cmd := m.cycleTab(key == "shift+tab")
			return true, next, cmd
		}
		return false, m, nil
	}

	if key == "q" && !m.typing() && !m.inConfirmModal() {
		return true, m, tea.Quit
	}
	if key == "f" && !m.typing() {
		next, cmd := m.cycleModelForTab()
		return true, next, cmd
	}
	if key == "R" && !m.typing() {
		next, cmd := m.reloadTab()
		return true, next, cmd
	}
	if key == "esc" {
		next, cmd := m.handleEsc()
		return true, next, cmd
	}
	return false, m, nil
}
