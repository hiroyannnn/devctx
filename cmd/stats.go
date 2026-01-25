package cmd

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show statistics about your contexts",
	Long:  `Display statistics including context counts by status, total time, and project breakdown.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		if len(store.Contexts) == 0 {
			fmt.Println("No contexts found.")
			return nil
		}

		titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		valueStyle := lipgloss.NewStyle().Bold(true)
		projectStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))

		// Count by status
		statusCounts := make(map[model.Status]int)
		for _, ctx := range store.Contexts {
			statusCounts[ctx.Status]++
		}

		fmt.Println(titleStyle.Render("📊 Context Statistics"))
		fmt.Println()

		// Overview
		fmt.Println(labelStyle.Render("Overview"))
		fmt.Printf("  Total contexts: %s\n", valueStyle.Render(fmt.Sprintf("%d", len(store.Contexts))))
		fmt.Println()

		// By status
		fmt.Println(labelStyle.Render("By Status"))
		statusOrder := []model.Status{model.StatusInProgress, model.StatusReview, model.StatusBlocked, model.StatusDone}
		statusEmoji := map[model.Status]string{
			model.StatusInProgress: "🚀",
			model.StatusReview:     "👀",
			model.StatusBlocked:    "🚧",
			model.StatusDone:       "✅",
		}
		for _, status := range statusOrder {
			count := statusCounts[status]
			emoji := statusEmoji[status]
			fmt.Printf("  %s %-12s %s\n", emoji, status, valueStyle.Render(fmt.Sprintf("%d", count)))
		}
		fmt.Println()

		// Total time
		var totalTime time.Duration
		for _, ctx := range store.Contexts {
			totalTime += ctx.TotalTime
		}
		if totalTime > 0 {
			fmt.Println(labelStyle.Render("Total Time Tracked"))
			fmt.Printf("  ⏱ %s\n", valueStyle.Render(formatDurationLong(totalTime)))
			fmt.Println()
		}

		// By project (group by worktree parent directory)
		projectStats := make(map[string]*projectStat)
		for _, ctx := range store.Contexts {
			project := getProjectName(ctx.Worktree)
			if project == "" {
				project = "(unknown)"
			}
			if _, ok := projectStats[project]; !ok {
				projectStats[project] = &projectStat{}
			}
			projectStats[project].count++
			projectStats[project].totalTime += ctx.TotalTime
		}

		// Sort projects by count
		type projectEntry struct {
			name string
			stat *projectStat
		}
		var projects []projectEntry
		for name, stat := range projectStats {
			projects = append(projects, projectEntry{name, stat})
		}
		sort.Slice(projects, func(i, j int) bool {
			return projects[i].stat.count > projects[j].stat.count
		})

		fmt.Println(labelStyle.Render("By Project"))
		maxDisplay := 10
		for i, p := range projects {
			if i >= maxDisplay {
				fmt.Printf("  ... and %d more projects\n", len(projects)-maxDisplay)
				break
			}
			timeStr := ""
			if p.stat.totalTime > 0 {
				timeStr = fmt.Sprintf(" (%s)", formatDurationLong(p.stat.totalTime))
			}
			fmt.Printf("  %s %s%s\n",
				projectStyle.Render(fmt.Sprintf("%-20s", truncate(p.name, 20))),
				valueStyle.Render(fmt.Sprintf("%3d", p.stat.count)),
				labelStyle.Render(timeStr))
		}
		fmt.Println()

		// Recent activity
		recentCutoff := time.Now().AddDate(0, 0, -7)
		recentCount := 0
		for _, ctx := range store.Contexts {
			if ctx.LastSeen.After(recentCutoff) {
				recentCount++
			}
		}
		fmt.Println(labelStyle.Render("Recent Activity (7 days)"))
		fmt.Printf("  Active contexts: %s\n", valueStyle.Render(fmt.Sprintf("%d", recentCount)))

		return nil
	},
}

type projectStat struct {
	count     int
	totalTime time.Duration
}

func getProjectName(worktree string) string {
	if worktree == "" {
		return ""
	}
	// Get the base directory name
	return filepath.Base(worktree)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func formatDurationLong(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func init() {
	rootCmd.AddCommand(statsCmd)
}
