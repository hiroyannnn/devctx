package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var Version = "dev"

var versionCheck bool

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of devctx",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("devctx version %s\n", Version)

		if versionCheck {
			checkForUpdates()
		}
	},
}

func init() {
	versionCmd.Flags().BoolVar(&versionCheck, "check", false, "Check for available updates")
}

func checkForUpdates() {
	if Version == "dev" {
		fmt.Println("Development build, skipping update check.")
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to check for updates: %v\n", err)
		return
	}

	checker := &UpdateChecker{
		CurrentVersion: Version,
		CachePath:      filepath.Join(home, ".config", "devctx", "update-check.yaml"),
		APIURL:         "https://api.github.com/repos/hiroyannnn/devctx/releases/latest",
	}

	latest, err := checker.FetchLatestVersion()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to check for updates: %v\n", err)
		return
	}

	// Save cache
	checker.SaveCache(&UpdateCache{
		LastCheckedAt: time.Now(),
		LatestVersion: latest,
		CheckedOK:     true,
	})

	if checker.IsNewer(latest, Version) {
		fmt.Println(checker.NotifyMessage(latest))
	} else {
		fmt.Printf("devctx %s is up to date.\n", Version)
	}
}
