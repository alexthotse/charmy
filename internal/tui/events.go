package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/opencode-ai/opencode/internal/config"
	"github.com/opencode-ai/opencode/internal/llm/agent"
	"github.com/opencode-ai/opencode/internal/logging"
	"github.com/opencode-ai/opencode/internal/permission"
	"github.com/opencode-ai/opencode/internal/pubsub"
	"github.com/opencode-ai/opencode/internal/session"
	"github.com/opencode-ai/opencode/internal/tui/components/chat"
	"github.com/opencode-ai/opencode/internal/tui/components/core"
	"github.com/opencode-ai/opencode/internal/tui/components/dialog"
	"github.com/opencode-ai/opencode/internal/tui/page"
	"github.com/opencode-ai/opencode/internal/tui/state"
	"github.com/opencode-ai/opencode/internal/tui/util"
	"os/exec"
)

func (a *state.AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		msg.Height -= 1 // Make space for the status bar
		a.Width, a.Height = msg.Width, msg.Height

		s, _ := a.Status.Update(msg)
		a.Status = s.(core.StatusCmp)
		a.Pages[a.CurrentPage], cmd = a.Pages[a.CurrentPage].Update(msg)
		cmds = append(cmds, cmd)

		prm, permCmd := a.Dialogs.Permissions.Update(msg)
		a.Dialogs.Permissions = prm.(dialog.PermissionDialogCmp)
		cmds = append(cmds, permCmd)

		help, helpCmd := a.Dialogs.Help.Update(msg)
		a.Dialogs.Help = help.(dialog.HelpCmp)
		cmds = append(cmds, helpCmd)

		session, sessionCmd := a.Dialogs.Session.Update(msg)
		a.Dialogs.Session = session.(dialog.SessionDialog)
		cmds = append(cmds, sessionCmd)

		command, commandCmd := a.Dialogs.Command.Update(msg)
		a.Dialogs.Command = command.(dialog.CommandDialog)
		cmds = append(cmds, commandCmd)

		filepicker, filepickerCmd := a.Dialogs.Filepicker.Update(msg)
		a.Dialogs.Filepicker = filepicker.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)

		a.Dialogs.Init.SetSize(msg.Width, msg.Height)

		if a.ShowMultiArguments {
			a.Dialogs.MultiArguments.SetSize(msg.Width, msg.Height)
			args, argsCmd := a.Dialogs.MultiArguments.Update(msg)
			a.Dialogs.MultiArguments = args.(dialog.MultiArgumentsDialogCmp)
			cmds = append(cmds, argsCmd, a.Dialogs.MultiArguments.Init())
		}

		return a, tea.Batch(cmds...)
	// Status
	case util.InfoMsg:
		s, cmd := a.Status.Update(msg)
		a.Status = s.(core.StatusCmp)
		cmds = append(cmds, cmd)
		return a, tea.Batch(cmds...)
	case pubsub.Event[logging.LogMessage]:
		if msg.Payload.Persist {
			switch msg.Payload.Level {
			case "error":
				s, cmd := a.Status.Update(util.InfoMsg{
					Type: util.InfoTypeError,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.Status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			case "info":
				s, cmd := a.Status.Update(util.InfoMsg{
					Type: util.InfoTypeInfo,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.Status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)

			case "warn":
				s, cmd := a.Status.Update(util.InfoMsg{
					Type: util.InfoTypeWarn,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})

				a.Status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			default:
				s, cmd := a.Status.Update(util.InfoMsg{
					Type: util.InfoTypeInfo,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.Status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			}
		}
	case util.ClearStatusMsg:
		s, _ := a.Status.Update(msg)
		a.Status = s.(core.StatusCmp)

	// Permission
	case pubsub.Event[permission.PermissionRequest]:
		a.ShowPermissions = true
		return a, a.Dialogs.Permissions.SetPermissions(msg.Payload)
	case dialog.PermissionResponseMsg:
		var cmd tea.Cmd
		switch msg.Action {
		case dialog.PermissionAllow:
			a.App.Permissions.Grant(msg.Permission)
		case dialog.PermissionAllowForSession:
			a.App.Permissions.GrantPersistant(msg.Permission)
		case dialog.PermissionDeny:
			a.App.Permissions.Deny(msg.Permission)
		}
		a.ShowPermissions = false
		return a, cmd

	case page.PageChangeMsg:
		return a, a.moveToPage(msg.ID)

	case dialog.CloseQuitMsg:
		a.ShowQuit = false
		return a, nil

	case dialog.CloseSessionDialogMsg:
		a.ShowSession = false
		return a, nil

	case dialog.CloseCommandDialogMsg:
		a.ShowCommand = false
		return a, nil

	case startCompactSessionMsg:
		// Start compacting the current session
		a.IsCompacting = true
		a.CompactingMessage = "Starting summarization..."

		if a.SelectedSession.ID == "" {
			a.IsCompacting = false
			return a, util.ReportWarn("No active session to summarize")
		}

		// Start the summarization process
		return a, func() tea.Msg {
			ctx := context.Background()
			a.App.CoderAgent.Summarize(ctx, a.SelectedSession.ID)
			return nil
		}

	case pubsub.Event[agent.AgentEvent]:
		payload := msg.Payload
		if payload.Error != nil {
			a.IsCompacting = false
			return a, util.ReportError(payload.Error)
		}

		a.CompactingMessage = payload.Progress

		if payload.Done && payload.Type == agent.AgentEventTypeSummarize {
			a.IsCompacting = false
			return a, util.ReportInfo("Session summarization complete")
		} else if payload.Done && payload.Type == agent.AgentEventTypeResponse && a.SelectedSession.ID != "" {
			model := a.App.CoderAgent.Model()
			contextWindow := model.ContextWindow
			tokens := a.SelectedSession.CompletionTokens + a.SelectedSession.PromptTokens
			if (tokens >= int64(float64(contextWindow)*0.95)) && config.Get().AutoCompact {
				return a, util.CmdHandler(startCompactSessionMsg{})
			}
		}
		// Continue listening for events
		return a, nil

	case dialog.CloseThemeDialogMsg:
		a.ShowTheme = false
		return a, nil

	case dialog.ThemeChangedMsg:
		a.Pages[a.CurrentPage], cmd = a.Pages[a.CurrentPage].Update(msg)
		a.ShowTheme = false
		return a, tea.Batch(cmd, util.ReportInfo("Theme changed to: "+msg.ThemeName))

	case dialog.CloseModelDialogMsg:
		a.ShowModel = false
		return a, nil

	case dialog.ModelSelectedMsg:
		a.ShowModel = false

		model, err := a.App.CoderAgent.Update(config.AgentCoder, msg.Model.ID)
		if err != nil {
			return a, util.ReportError(err)
		}

		return a, util.ReportInfo(fmt.Sprintf("Model changed to %s", model.Name))

	case dialog.ShowInitDialogMsg:
		a.ShowInit = msg.Show
		return a, nil

	case dialog.CloseInitDialogMsg:
		a.ShowInit = false
		if msg.Initialize {
			// Run the initialization command
			for _, cmd := range a.commands {
				if cmd.ID == "init" {
					// Mark the project as initialized
					if err := config.MarkProjectInitialized(); err != nil {
						return a, util.ReportError(err)
					}
					return a, cmd.Handler(cmd)
				}
			}
		} else {
			// Mark the project as initialized without running the command
			if err := config.MarkProjectInitialized(); err != nil {
				return a, util.ReportError(err)
			}
		}
		return a, nil

	case chat.SessionSelectedMsg:
		a.SelectedSession = msg
		a.Dialogs.Session.SetSelectedSession(msg.ID)

	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == a.SelectedSession.ID {
			a.SelectedSession = msg.Payload
		}
	case dialog.SessionSelectedMsg:
		a.ShowSession = false
		if a.CurrentPage == page.ChatPage {
			return a, util.CmdHandler(chat.SessionSelectedMsg(msg.Session))
		}
		return a, nil

	case dialog.CommandSelectedMsg:
		a.ShowCommand = false
		// Execute the command handler if available
		if msg.Command.Handler != nil {
			return a, msg.Command.Handler(msg.Command)
		}
		return a, util.ReportInfo("Command selected: " + msg.Command.Title)

	case dialog.ShowMultiArgumentsDialogMsg:
		// Show multi-arguments dialog
		a.Dialogs.MultiArguments = dialog.NewMultiArgumentsDialogCmp(msg.CommandID, msg.Content, msg.ArgNames)
		a.ShowMultiArguments = true
		return a, a.Dialogs.MultiArguments.Init()

	case dialog.CloseMultiArgumentsDialogMsg:
		// Close multi-arguments dialog
		a.ShowMultiArguments = false

		// If submitted, replace all named arguments and run the command
		if msg.Submit {
			content := msg.Content

			// Replace each named argument with its value
			for name, value := range msg.Args {
				placeholder := "$" + name
				content = strings.ReplaceAll(content, placeholder, value)
			}

			// Execute the command with arguments
			return a, util.CmdHandler(dialog.CommandRunCustomMsg{
				Content: content,
				Args:    msg.Args,
			})
		}
		return a, nil

	case tea.Msg:
		switch msg {
		case "setup-agent-os":
			cmd := exec.Command("bash", "-c", "curl -fsSL https://raw.githubusercontent.com/buildermethods/agent-os/main/install | bash")
			err := cmd.Run()
			if err != nil {
				return a, util.ReportError(fmt.Errorf("failed to install Agent OS: %w", err))
			}
			return a, util.ReportInfo("Agent OS installed successfully")
		}
	case tea.KeyMsg:
		// If multi-arguments dialog is open, let it handle the key press first
		if a.ShowMultiArguments {
			args, cmd := a.Dialogs.MultiArguments.Update(msg)
			a.Dialogs.MultiArguments = args.(dialog.MultiArgumentsDialogCmp)
			return a, cmd
		}

		switch {

		case key.Matches(msg, keys.Quit):
			a.ShowQuit = !a.ShowQuit
			if a.ShowHelp {
				a.ShowHelp = false
			}
			if a.ShowSession {
				a.ShowSession = false
			}
			if a.ShowCommand {
				a.ShowCommand = false
			}
			if a.ShowFilepicker {
				a.ShowFilepicker = false
				a.Dialogs.Filepicker.ToggleFilepicker(a.ShowFilepicker)
			}
			if a.ShowModel {
				a.ShowModel = false
			}
			if a.ShowMultiArguments {
				a.ShowMultiArguments = false
			}
			return a, nil
		case key.Matches(msg, keys.SwitchSession):
			if a.CurrentPage == page.ChatPage && !a.ShowQuit && !a.ShowPermissions && !a.ShowCommand {
				// Load sessions and show the dialog
				sessions, err := a.App.Sessions.List(context.Background())
				if err != nil {
					return a, util.ReportError(err)
				}
				if len(sessions) == 0 {
					return a, util.ReportWarn("No sessions available")
				}
				a.Dialogs.Session.SetSessions(sessions)
				a.ShowSession = true
				return a, nil
			}
			return a, nil
		case key.Matches(msg, keys.Commands):
			if a.CurrentPage == page.ChatPage && !a.ShowQuit && !a.ShowPermissions && !a.ShowSession && !a.ShowTheme && !a.ShowFilepicker {
				// Show commands dialog
				if len(a.commands) == 0 {
					return a, util.ReportWarn("No commands available")
				}
				a.Dialogs.Command.SetCommands(a.commands)
				a.ShowCommand = true
				return a, nil
			}
			return a, nil
		case key.Matches(msg, keys.Models):
			if a.ShowModel {
				a.ShowModel = false
				return a, nil
			}
			if a.CurrentPage == page.ChatPage && !a.ShowQuit && !a.ShowPermissions && !a.ShowSession && !a.ShowCommand {
				a.ShowModel = true
				return a, nil
			}
			return a, nil
		case key.Matches(msg, keys.SwitchTheme):
			if !a.ShowQuit && !a.ShowPermissions && !a.ShowSession && !a.ShowCommand {
				// Show theme switcher dialog
				a.ShowTheme = true
				// Theme list is dynamically loaded by the dialog component
				return a, a.Dialogs.Theme.Init()
			}
			return a, nil
		case key.Matches(msg, returnKey) || key.Matches(msg):
			if msg.String() == quitKey {
				if a.CurrentPage == page.LogsPage {
					return a, a.moveToPage(page.ChatPage)
				}
			} else if !a.Dialogs.Filepicker.IsCWDFocused() {
				if a.ShowQuit {
					a.ShowQuit = !a.ShowQuit
					return a, nil
				}
				if a.ShowHelp {
					a.ShowHelp = !a.ShowHelp
					return a, nil
				}
				if a.ShowInit {
					a.ShowInit = false
					// Mark the project as initialized without running the command
					if err := config.MarkProjectInitialized(); err != nil {
						return a, util.ReportError(err)
					}
					return a, nil
				}
				if a.ShowFilepicker {
					a.ShowFilepicker = false
					a.Dialogs.Filepicker.ToggleFilepicker(a.ShowFilepicker)
					return a, nil
				}
				if a.CurrentPage == page.LogsPage {
					return a, a.moveToPage(page.ChatPage)
				}
			}
		case key.Matches(msg, keys.Logs):
			return a, a.moveToPage(page.LogsPage)
		case key.Matches(msg, keys.Help):
			if a.ShowQuit {
				return a, nil
			}
			a.ShowHelp = !a.ShowHelp
			return a, nil
		case key.Matches(msg, helpEsc):
			if a.App.CoderAgent.IsBusy() {
				if a.ShowQuit {
					return a, nil
				}
				a.ShowHelp = !a.ShowHelp
				return a, nil
			}
		case key.Matches(msg, keys.Filepicker):
			a.ShowFilepicker = !a.ShowFilepicker
			a.Dialogs.Filepicker.ToggleFilepicker(a.ShowFilepicker)
			return a, nil
		}
	default:
		f, filepickerCmd := a.Dialogs.Filepicker.Update(msg)
		a.Dialogs.Filepicker = f.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)

	}

	if a.ShowFilepicker {
		f, filepickerCmd := a.Dialogs.Filepicker.Update(msg)
		a.Dialogs.Filepicker = f.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.ShowQuit {
		q, quitCmd := a.Dialogs.Quit.Update(msg)
		a.Dialogs.Quit = q.(dialog.QuitDialog)
		cmds = append(cmds, quitCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}
	if a.ShowPermissions {
		d, permissionsCmd := a.Dialogs.Permissions.Update(msg)
		a.Dialogs.Permissions = d.(dialog.PermissionDialogCmp)
		cmds = append(cmds, permissionsCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.ShowSession {
		d, sessionCmd := a.Dialogs.Session.Update(msg)
		a.Dialogs.Session = d.(dialog.SessionDialog)
		cmds = append(cmds, sessionCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.ShowCommand {
		d, commandCmd := a.Dialogs.Command.Update(msg)
		a.Dialogs.Command = d.(dialog.CommandDialog)
		cmds = append(cmds, commandCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.ShowModel {
		d, modelCmd := a.Dialogs.Model.Update(msg)
		a.Dialogs.Model = d.(dialog.ModelDialog)
		cmds = append(cmds, modelCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.ShowInit {
		d, initCmd := a.Dialogs.Init.Update(msg)
		a.Dialogs.Init = d.(dialog.InitDialogCmp)
		cmds = append(cmds, initCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.ShowTheme {
		d, themeCmd := a.Dialogs.Theme.Update(msg)
		a.Dialogs.Theme = d.(dialog.ThemeDialog)
		cmds = append(cmds, themeCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	s, _ := a.Status.Update(msg)
	a.Status = s.(core.StatusCmp)
	a.Pages[a.CurrentPage], cmd = a.Pages[a.CurrentPage].Update(msg)
	cmds = append(cmds, cmd)
	return a, tea.Batch(cmds...)
}
