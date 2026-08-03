package cmd

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/Ibnu-Afdel/dbwiz/internal/keymap"
)

// NewKeysCommand builds `dbwiz keys`: the interactive UI's whole keymap, printed.
//
// It reads the same bindings the TUI's handlers match on (internal/keymap), so
// this listing cannot describe a key as something it isn't — there is only one
// keymap and both readers share it. Useful for a cheat sheet next to the
// terminal, and for anyone who'd rather read the keys before launching the UI.
func NewKeysCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "keys",
		Short: "List every key the interactive UI answers to",
		Long: "Print the full keymap of the interactive UI, grouped by what the keys do.\n\n" +
			"Inside DBWiz the same list is on F1, and `?` expands the bottom bar to just\n" +
			"the keys that work wherever you happen to be.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := io.WriteString(cmd.OutOrStdout(), keymap.Render())
			return err
		},
	}
}
