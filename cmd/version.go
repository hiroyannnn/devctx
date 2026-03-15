package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var Version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of devctx",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("devctx version %s\n", Version)
	},
}
