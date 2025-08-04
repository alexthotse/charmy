package state

import (
	"github.com/opencode-ai/opencode/internal/app"
	"github.com/opencode-ai/opencode/internal/session"
	"github.com/opencode-ai/opencode/internal/tui/components/core"
	"github.com/opencode-ai/opencode/internal/tui/components/dialog"
	"github.com/opencode-ai/opencode/internal/tui/page"
	tea "github.com/charmbracelet/bubbletea"
)

type Dialogs struct {
	Permissions          dialog.PermissionDialogCmp
	Help                 dialog.HelpCmp
	Quit                 dialog.QuitDialog
	Session              dialog.SessionDialog
	Command              dialog.CommandDialog
	Model                dialog.ModelDialog
	Init                 dialog.InitDialogCmp
	Filepicker           dialog.FilepickerCmp
	Theme                dialog.ThemeDialog
	MultiArguments       dialog.MultiArgumentsDialogCmp
}

type AppModel struct {
	Width, Height   int
	CurrentPage     page.PageID
	PreviousPage    page.PageID
	Pages           map[page.PageID]tea.Model
	LoadedPages     map[page.PageID]bool
	Status          core.StatusCmp
	App             *app.App
	SelectedSession session.Session
	Dialogs         Dialogs
	ShowHelp        bool
	ShowQuit        bool
	ShowSession     bool
	ShowCommand     bool
	ShowModel       bool
	ShowInit        bool
	ShowFilepicker  bool
	ShowTheme       bool
	ShowMultiArguments bool
	IsCompacting      bool
	CompactingMessage string
}
