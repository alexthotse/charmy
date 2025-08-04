package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/opencode-ai/opencode/internal/app"
	"github.com/opencode-ai/opencode/internal/config"
	"github.com/opencode-ai/opencode/internal/llm/agent"
	"github.com/opencode-ai/opencode/internal/logging"
	"github.com/opencode-ai/opencode/internal/session"
	"github.com/opencode-ai/opencode/internal/tui/components/chat"
	"github.com/opencode-ai/opencode/internal/tui/components/core"
	"github.com/opencode-ai/opencode/internal/tui/components/dialog"
	"github.com/opencode-ai/opencode/internal/tui/page"
	"github.com/opencode-ai/opencode/internal/tui/state"
	"github.com/opencode-ai/opencode/internal/tui/util"
	"os"
	"path/filepath"

	"os/exec"
)

type keyMap struct {
	Logs          key.Binding
	Quit          key.Binding
	Help          key.Binding
	SwitchSession key.Binding
	Commands      key.Binding
	Filepicker    key.Binding
	Models        key.Binding
	SwitchTheme   key.Binding
}

type startCompactSessionMsg struct{}

const (
	quitKey = "q"
)

var keys = keyMap{
	Logs: key.NewBinding(
		key.WithKeys("ctrl+l"),
		key.WithHelp("ctrl+l", "logs"),
	),

	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	Help: key.NewBinding(
		key.WithKeys("ctrl+_", "ctrl+h", "ctrl+?"),
		key.WithHelp("ctrl+?", "toggle help"),
	),

	SwitchSession: key.NewBinding(
		key.WithKeys("ctrl+s"),
		key.WithHelp("ctrl+s", "switch session"),
	),

	Commands: key.NewBinding(
		key.WithKeys("ctrl+k"),
		key.WithHelp("ctrl+k", "commands"),
	),
	Filepicker: key.NewBinding(
		key.WithKeys("ctrl+f"),
		key.WithHelp("ctrl+f", "select files to upload"),
	),
	Models: key.NewBinding(
		key.WithKeys("ctrl+o"),
		key.WithHelp("ctrl+o", "model selection"),
	),

	SwitchTheme: key.NewBinding(
		key.WithKeys("ctrl+t"),
		key.WithHelp("ctrl+t", "switch theme"),
	),
}

var helpEsc = key.NewBinding(
	key.WithKeys("?"),
	key.WithHelp("?", "toggle help"),
)

var returnKey = key.NewBinding(
	key.WithKeys("esc"),
	key.WithHelp("esc", "close"),
)

var logsKeyReturnKey = key.NewBinding(
	key.WithKeys("esc", "backspace", quitKey),
	key.WithHelp("esc/q", "go back"),
)

type appModel struct {
	*state.AppModel
}

func (a *appModel) Init() tea.Cmd {
	return a.AppModel.Init()
}

func (a *appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return a.AppModel.Update(msg)
}

func (a *appModel) View() string {
	return a.AppModel.View()
}

func New(app *app.App) tea.Model {
	startPage := page.ChatPage
	model := &appModel{
		AppModel: &state.AppModel{
			CurrentPage: startPage,
			LoadedPages: make(map[page.PageID]bool),
			Status:      core.NewStatusCmp(app.LSPClients),
			Dialogs: state.Dialogs{
				Help:        dialog.NewHelpCmp(),
				Quit:        dialog.NewQuitCmp(),
				Session:     dialog.NewSessionDialogCmp(),
				Command:     dialog.NewCommandDialogCmp(),
				Model:       dialog.NewModelDialogCmp(),
				Permissions: dialog.NewPermissionDialogCmp(),
				Init:        dialog.NewInitDialogCmp(),
				Theme:       dialog.NewThemeDialogCmp(),
				Filepicker:  dialog.NewFilepickerCmp(app),
			},
			App: app,
			Pages: map[page.PageID]tea.Model{
				page.ChatPage: page.NewChatPage(app),
				page.LogsPage: page.NewLogsPage(),
			},
		},
	}

	model.RegisterCommand(dialog.Command{
		ID:          "init",
		Title:       "Initialize Project",
		Description: "Create/Update the OpenCode.md memory file",
		Handler: func(cmd dialog.Command) tea.Cmd {
			// Check for global Agent OS installation
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return util.ReportError(fmt.Errorf("failed to get home directory: %w", err))
			}
			agentOsDir := filepath.Join(homeDir, "agent_os")
			if _, err := os.Stat(agentOsDir); os.IsNotExist(err) {
				return util.ReportWarn("Agent OS not found. Please run the 'Setup Agent OS' command from the command menu (ctrl+k).")
			}

			// Create project-specific agent_os directory
			wd, err := os.Getwd()
			if err != nil {
				return util.ReportError(fmt.Errorf("failed to get working directory: %w", err))
			}
			projectAgentOsDir := filepath.Join(wd, "agent_os")
			if _, err := os.Stat(projectAgentOsDir); os.IsNotExist(err) {
				if err := os.MkdirAll(projectAgentOsDir, 0755); err != nil {
					return util.ReportError(fmt.Errorf("failed to create agent_os directory: %w", err))
				}
			}

			prompt := `Please analyze this codebase and create a OpenCode.md file containing:
1. Build/lint/test commands - especially for running a single test
2. Code style guidelines including imports, formatting, types, naming conventions, error handling, etc.

The file you create will be given to agentic coding agents (such as yourself) that operate in this repository. Make it about 20 lines long.
If there's already a opencode.md, improve it.
If there are Cursor rules (in .cursor/rules/ or .cursorrules) or Copilot rules (in .github/copilot-instructions.md), make sure to include them.`

			// Generate project planning documents
			planningPrompt := "Plan out a new product based on the current project context, referencing the Agent OS instructions and standards."

			return tea.Batch(
				util.CmdHandler(chat.SendMsg{
					Text: prompt,
				}),
				util.CmdHandler(chat.SendMsg{
					Text: planningPrompt,
				}),
				createAgentOsCommands(),
			)
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "compact",
		Title:       "Compact Session",
		Description: "Summarize the current session and create a new one with the summary",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				return startCompactSessionMsg{}
			}
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "setup-agent-os",
		Title:       "Setup Agent OS",
		Description: "Install Agent OS",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(tea.Msg("setup-agent-os"))
		},
	})
	// Load custom commands
	customCommands, err := dialog.LoadCustomCommands()
	if err != nil {
		logging.Warn("Failed to load custom commands", "error", err)
	} else {
		for _, cmd := range customCommands {
			model.RegisterCommand(cmd)
		}
	}

	return model
}

func createAgentOsCommands() tea.Cmd {
	return func() tea.Msg {
		wd, err := os.Getwd()
		if err != nil {
			return util.InfoMsg{Type: util.InfoTypeError, Msg: fmt.Sprintf("failed to get working directory: %v", err)}
		}

		commandsDir := filepath.Join(wd, ".opencode", "commands")
		if _, err := os.Stat(commandsDir); os.IsNotExist(err) {
			if err := os.MkdirAll(commandsDir, 0755); err != nil {
				return util.InfoMsg{Type: util.InfoTypeError, Msg: fmt.Sprintf("failed to create commands directory: %v", err)}
			}
		}

		agentOsCommands := []string{"analyze_product", "create_spec", "execute_tasks", "plan_product"}
		for _, cmd := range agentOsCommands {
			cmdFile := filepath.Join(commandsDir, cmd+".md")
			if _, err := os.Stat(cmdFile); os.IsNotExist(err) {
				content := fmt.Sprintf("@~/agent_os/instructions/%s.md", cmd)
				if err := os.WriteFile(cmdFile, []byte(content), 0644); err != nil {
					return util.InfoMsg{Type: util.InfoTypeError, Msg: fmt.Sprintf("failed to create command file: %v", err)}
				}
			}
		}
		return util.InfoMsg{Type: util.InfoTypeInfo, Msg: "Agent OS project initialized successfully"}
	}
}

func (a *state.AppModel) moveToPage(pageID page.PageID) tea.Cmd {
	if a.App.CoderAgent.IsBusy() {
		// For now we don't move to any page if the agent is busy
		return util.ReportWarn("Agent is busy, please wait...")
	}

	var cmds []tea.Cmd
	if _, ok := a.LoadedPages[pageID]; !ok {
		cmd := a.Pages[pageID].Init()
		cmds = append(cmds, cmd)
		a.LoadedPages[pageID] = true
	}
	a.PreviousPage = a.CurrentPage
	a.CurrentPage = pageID
	if sizable, ok := a.Pages[a.CurrentPage].(layout.Sizeable); ok {
		cmd := sizable.SetSize(a.Width, a.Height)
		cmds = append(cmds, cmd)
	}

	return tea.Batch(cmds...)
}

func (a *state.AppModel) RegisterCommand(cmd dialog.Command) {
	a.Commands = append(a.Commands, cmd)
}

func (a *state.AppModel) findCommand(id string) (dialog.Command, bool) {
	for _, cmd := range a.Commands {
		if cmd.ID == id {
			return cmd, true
		}
	}
	return dialog.Command{}, false
}
