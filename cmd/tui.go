package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hiroyannnn/devctx/agentview"
	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:     "tui",
	Aliases: []string{"ui", "dashboard"},
	Short:   "Open interactive TUI dashboard",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		p := tea.NewProgram(newTuiModel(store, s), tea.WithAltScreen())
		finalModel, err := p.Run()
		if err != nil {
			return err
		}

		// If user selected a context to resume
		if m, ok := finalModel.(tuiModel); ok && m.selectedForResume != nil {
			shellCmd, err := resumeShellCommand(*m.selectedForResume)
			if err != nil {
				return err
			}
			fmt.Println(shellCmd)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}

// Item implements list.Item
type contextItem struct {
	ctx  model.Context
	view agentview.View
}

func (i contextItem) Title() string {
	icon := statusIcon(i.ctx.Status)
	return fmt.Sprintf("%s [%s]", icon, i.ctx.Name)
}

func (i contextItem) Description() string {
	parts := []string{agentTag(i.ctx, i.view), i.ctx.Branch}
	if i.ctx.Note != "" {
		note := i.ctx.Note
		if len(note) > 40 {
			note = note[:40] + "..."
		}
		parts = append(parts, "📝 "+note)
	}
	parts = append(parts, "⏱ "+formatRelativeTime(i.ctx.LastSeen))
	if i.ctx.TotalTime > 0 {
		parts = append(parts, "⌛ "+formatDuration(i.ctx.TotalTime))
	}
	return strings.Join(parts, " | ")
}

func (i contextItem) FilterValue() string {
	return i.ctx.Name + " " + i.ctx.Branch
}

type tuiModel struct {
	list              list.Model
	store             *model.Store
	storage           *storage.Storage
	selectedForResume *model.Context
	err               error
	live              *liveViews
}

// tuiRedrawInterval は操作がなくても live 状態を描き直す間隔。取得自体は Refresher が
// バックグラウンドで行うので、tick は直近の snapshot を読んで再描画するだけ。
// Update 内で同期取得しない理由: claude の実行（150ms〜2s）が UI を止めるため。
const tuiRedrawInterval = 2 * time.Second

// tuiTickMsg は定期再描画の合図。
type tuiTickMsg struct{}

func tuiTickCmd() tea.Cmd {
	return tea.Tick(tuiRedrawInterval, func(time.Time) tea.Msg { return tuiTickMsg{} })
}

type keyMap struct {
	Enter    key.Binding
	Move     key.Binding
	Note     key.Binding
	Delete   key.Binding
	Quit     key.Binding
	Help     key.Binding
	Review   key.Binding
	Done     key.Binding
	Blocked  key.Binding
	Progress key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "resume")),
		Move:     key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "move")),
		Note:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "note")),
		Delete:   key.NewBinding(key.WithKeys("d", "delete"), key.WithHelp("d", "delete")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Review:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "→review")),
		Done:     key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "→done")),
		Blocked:  key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "→blocked")),
		Progress: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "→progress")),
	}
}

var keys = newKeyMap()

// buildItems は status 順の一覧を作る。live 状態は表示用の view としてのみ持ち、store には入れない。
func buildItems(store *model.Store, views map[string]agentview.View) []list.Item {
	items := make([]list.Item, 0)
	statuses := []model.Status{
		model.StatusInProgress,
		model.StatusReview,
		model.StatusBlocked,
		model.StatusDone,
	}
	for _, status := range statuses {
		for _, ctx := range store.ByStatus(status) {
			items = append(items, contextItem{ctx: ctx, view: views[ctx.Name]})
		}
	}
	return items
}

func newTuiModel(store *model.Store, s *storage.Storage) tuiModel {
	// 初回は取得前なので hook 状態で描き、以降は tick で Refresher の snapshot を取り込む
	live := newWatchLiveViews()
	items := buildItems(store, live.views(store.Contexts))

	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(lipgloss.Color("39"))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(lipgloss.Color("244"))

	l := list.New(items, delegate, 80, 20)
	l.Title = "devctx - Development Contexts"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.Styles.Title = lipgloss.NewStyle().
		Background(lipgloss.Color("62")).
		Foreground(lipgloss.Color("230")).
		Padding(0, 1)

	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			keys.Enter,
			keys.Review,
			keys.Done,
			keys.Blocked,
			keys.Progress,
		}
	}

	return tuiModel{
		list:    l,
		store:   store,
		storage: s,
		live:    live,
	}
}

func (m tuiModel) Init() tea.Cmd {
	return tuiTickCmd()
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetWidth(msg.Width)
		m.list.SetHeight(msg.Height - 2)
		return m, nil

	case tuiTickMsg:
		m.list.SetItems(buildItems(m.store, m.live.views(m.store.Contexts)))
		return m, tuiTickCmd()

	case tea.KeyMsg:
		// Don't handle keys if filtering
		if m.list.FilterState() == list.Filtering {
			break
		}

		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.Enter):
			if item, ok := m.list.SelectedItem().(contextItem); ok {
				selected := item.ctx
				m.selectedForResume = &selected
				return m, tea.Quit
			}

		case key.Matches(msg, keys.Review):
			return m.moveSelected(model.StatusReview)

		case key.Matches(msg, keys.Done):
			return m.moveSelected(model.StatusDone)

		case key.Matches(msg, keys.Blocked):
			return m.moveSelected(model.StatusBlocked)

		case key.Matches(msg, keys.Progress):
			return m.moveSelected(model.StatusInProgress)

		case key.Matches(msg, keys.Delete):
			if item, ok := m.list.SelectedItem().(contextItem); ok {
				if saved, _, err := removeContexts(m.storage, item.ctx.Name); err == nil {
					m.store = saved
				}
				return m.refreshList()
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m tuiModel) moveSelected(status model.Status) (tea.Model, tea.Cmd) {
	if item, ok := m.list.SelectedItem().(contextItem); ok {
		// 起動時に読んだ m.store を丸ごと保存すると、その後に hook やダッシュボードが
		// 書いた agent_state / タスク紐付けを巻き戻すので、ロック内で status だけ変える
		saved, err := updateContext(m.storage, item.ctx.Name, func(ctx *model.Context) error {
			ctx.Status = status
			return nil
		})
		if err == nil {
			m.store = saved
			return m.refreshList()
		}
	}
	return m, nil
}

func (m tuiModel) refreshList() (tuiModel, tea.Cmd) {
	m.list.SetItems(buildItems(m.store, m.live.views(m.store.Contexts)))
	return m, nil
}

func (m tuiModel) View() string {
	return m.list.View()
}
