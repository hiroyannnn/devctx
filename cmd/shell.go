package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var shellInitCmd = &cobra.Command{
	Use:   "shell-init",
	Short: "Output shell integration script",
	Long: `Output shell function for easier context resumption.
Add to your .bashrc or .zshrc:

  eval "$(devctx shell-init)"

Then use:
  dx auth      # Resume context "auth"
  dxl          # List contexts
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		script := `
# devctx shell integration
dx() {
    if [ -z "$1" ]; then
        devctx list
        return
    fi
    eval "$(devctx resume-shell "$1")"
}

dxl() {
    devctx list "$@"
}

dxm() {
    devctx move "$@"
}

dxr() {
    devctx register "$@"
}

# Completion for dx command
_dx_completions() {
    local contexts
    contexts=$(devctx list --names-only 2>/dev/null)
    COMPREPLY=($(compgen -W "$contexts" -- "${COMP_WORDS[1]}"))
}
complete -F _dx_completions dx
`
		fmt.Println(script)
		return nil
	},
}

var listNamesOnly bool

func init() {
	rootCmd.AddCommand(shellInitCmd)
	listCmd.Flags().BoolVar(&listNamesOnly, "names-only", false, "Output only context names (for completion)")
}
