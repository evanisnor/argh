package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ShowHelpMsg is sent to the root model to make the help overlay visible.
// It is produced by the ":help" command and handled in Model.Update().
type ShowHelpMsg struct{}

// helpSection is a list of [key, description] pairs for one section of the help overlay.
type helpSection struct {
	title string
	rows  [][2]string
}

var helpSections = []helpSection{
	{
		title: "Keyboard Shortcuts",
		rows: [][2]string{
			{"j / ↓", "Move focus down within focused panel"},
			{"k / ↑", "Move focus up"},
			{"Tab", "Cycle between panels (My PRs → Review Queue → Watches)"},
			{"Enter / p", "Toggle detail pane for focused PR"},
			{"o", "Open focused PR in browser"},
			{"d", "Show diff for focused PR"},
			{"a", "Approve focused PR (Review Queue only)"},
			{"r", "Open reviewer picker for focused PR"},
			{"?", "Toggle help overlay"},
			{"q", "Quit"},
			{"R", "Force reload (:reload)"},
			{"D", "Toggle Do Not Disturb"},
			{"/ or :", "Focus command bar"},
			{"Esc", "Dismiss overlay or unfocus command bar"},
		},
	},
	{
		title: "Commands",
		rows: [][2]string{
			{":open [#pr]", "Open PR in default browser"},
			{":diff [#pr]", "Show diff with delta pager"},
			{":approve [#pr]", "Submit approval review"},
			{":review [#pr]", "Open inline compose view (body + submit)"},
			{":request [#pr] @user...", "Request reviewers"},
			{":ready [#pr]", "Mark draft ready for review"},
			{":draft [#pr]", "Convert PR to draft"},
			{":merge [#pr]", "Merge PR using repo's configured method"},
			{":watch [#pr] <trigger> <action>", "Create a watch"},
			{":close [#pr]", "Close PR"},
			{":reopen [#pr]", "Reopen PR"},
			{":label [#pr] [label]", "Add or remove label"},
			{":comment [#pr]", "Open inline editor and post comment"},
			{":dnd [duration]", "Toggle Do Not Disturb (e.g. :dnd 2h)"},
			{":wake", "Resume normal polling"},
			{":reload", "Force immediate poll"},
			{":help", "Show this help overlay"},
			{":quit / q", "Exit argh"},
		},
	},
	{
		title: "Watch Trigger Syntax",
		rows: [][2]string{
			{"on:ci-pass", "CI passed"},
			{"on:ci-fail", "CI failed"},
			{"on:approved", "PR approved"},
			{"on:approved:N", "At least N approvals"},
			{"on:all-threads-resolved", "All review threads resolved"},
			{"on:ready-for-review", "PR marked ready for review"},
			{"on:label-added:<name>", "Label added"},
			{"on:label-removed:<name>", "Label removed"},
			{"on:<N>h-stale", "PR idle for N hours"},
			{"on:ci-pass+approved", "AND: ci-pass AND approved"},
			{"on:ci-pass,approved", "OR: ci-pass OR approved"},
		},
	},
	{
		title: "Watch Action Syntax",
		rows: [][2]string{
			{"merge", "Merge using repo's default strategy"},
			{"merge:squash", "Merge with squash"},
			{"merge:rebase", "Merge with rebase"},
			{"ready", "Mark draft ready for review"},
			{"request:@user", "Request a reviewer"},
			{"comment:<text>", "Post a comment"},
			{"label:<name>", "Add a label"},
			{"notify", "Send a desktop notification"},
			{"action1 + action2", "Combine multiple actions with +"},
		},
	},
}

// renderHelpContent builds the scrollable help text for the help overlay
// viewport. It returns a plain formatted string with all sections and rows;
// the caller is responsible for placing it inside a sized viewport and modal.
func renderHelpContent(theme Theme, version, username string) string {
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#C0C0FF"))
	if !theme.Dark {
		keyStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3030AA"))
	}
	descStyle := lipgloss.NewStyle()

	titleStyle := theme.PanelTitle

	var sb strings.Builder

	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("  argh — keyboard reference  "))
	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Faint(true).Render(fmt.Sprintf("  %s  @%s", version, username)))
	sb.WriteString("\n\n")

	for _, section := range helpSections {
		sb.WriteString(titleStyle.Render(section.title))
		sb.WriteString("\n")

		// Find the longest key so we can align descriptions.
		maxKey := 0
		for _, row := range section.rows {
			if len(row[0]) > maxKey {
				maxKey = len(row[0])
			}
		}

		for _, row := range section.rows {
			key := row[0]
			desc := row[1]
			padding := strings.Repeat(" ", maxKey-len(key)+2)
			sb.WriteString("  ")
			sb.WriteString(keyStyle.Render(key))
			sb.WriteString(padding)
			sb.WriteString(descStyle.Render(desc))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// dimBackground applies a faint/dim style to the normal layout string so it
// appears muted behind the help overlay.
func dimBackground(view string) string {
	return lipgloss.NewStyle().Faint(true).Render(view)
}
