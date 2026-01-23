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
    if [ "$1" = "-" ] || [ "$1" = "--last" ]; then
        # Resume last touched context
        local last
        last=$(devctx list --names-only 2>/dev/null | head -1)
        if [ -n "$last" ]; then
            eval "$(devctx resume-shell "$last")"
        else
            echo "No contexts found" >&2
            return 1
        fi
        return
    fi

    if [ -z "$1" ]; then
        # No argument: use fzf if available, otherwise list
        if command -v fzf >/dev/null 2>&1; then
            local selected
            selected=$(devctx list --fzf 2>/dev/null | fzf --ansi --header="Select context to resume" | awk '{print $1}')
            if [ -n "$selected" ]; then
                eval "$(devctx resume-shell "$selected")"
            fi
        else
            devctx list
        fi
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

dxn() {
    if [ -z "$1" ]; then
        echo "Usage: dxn <branch-name>" >&2
        return 1
    fi
    eval "$(devctx new-shell "$@")"
}

dxs() {
    devctx sync "$@"
}

dxt() {
    devctx tui "$@"
}

dxp() {
    devctx status "$@"
}

dxf() {
    devctx search "$@"
}

dxd() {
    devctx discover "$@"
}

# Completion for dx command
_dx_completions() {
    local contexts
    contexts=$(devctx list --names-only 2>/dev/null)
    COMPREPLY=($(compgen -W "$contexts -" -- "${COMP_WORDS[1]}"))
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
	listCmd.Flags().BoolVar(&listFzf, "fzf", false, "Output in fzf-friendly format")
}
