package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/estop"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/tmux"
	"github.com/steveyegge/gastown/internal/workspace"
)

var estopReason string

var estopCmd = &cobra.Command{
	Use:     "estop",
	GroupID: GroupServices,
	Short:   "Emergency stop — freeze all agent work",
	Long: `Emergency stop: freeze all agent sessions across the town.

This is the factory floor E-stop button. All agent sessions (crew, polecats,
witnesses, refineries, deacon, dogs) are sent SIGTSTP to freeze in place.
Context is preserved — no work is lost.

The Mayor is exempt so it can coordinate recovery.

To resume: gt thaw

Examples:
  gt estop                         # Freeze everything
  gt estop -r "Dolt server down"   # Freeze with reason`,
	RunE: runEstop,
}

var thawCmd = &cobra.Command{
	Use:     "thaw",
	GroupID: GroupServices,
	Short:   "Resume from emergency stop — thaw all frozen agents",
	Long: `Resume all agent sessions that were frozen by gt estop.

Sends SIGCONT to all frozen sessions, removes the ESTOP sentinel file,
and nudges all sessions to alert them that work can continue.

Examples:
  gt thaw`,
	RunE: runThaw,
}

func init() {
	estopCmd.Flags().StringVarP(&estopReason, "reason", "r", "", "Reason for the E-stop")
	rootCmd.AddCommand(estopCmd)
	rootCmd.AddCommand(thawCmd)
}

func runEstop(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	if estop.IsActive(townRoot) {
		info := estop.Read(townRoot)
		if info != nil {
			fmt.Printf("%s E-stop already active (triggered %s: %s)\n",
				style.Error.Render("⛔"), info.Trigger, info.Reason)
		}
		return nil
	}

	// Create the sentinel file first — this is the source of truth
	if err := estop.Activate(townRoot, estop.TriggerManual, estopReason); err != nil {
		return fmt.Errorf("failed to create ESTOP file: %w", err)
	}

	fmt.Printf("%s EMERGENCY STOP\n", style.Error.Render("⛔"))
	if estopReason != "" {
		fmt.Printf("   Reason: %s\n", estopReason)
	}
	fmt.Println()

	t := tmux.NewTmux()
	if !t.IsAvailable() {
		fmt.Printf("%s tmux not available — ESTOP file created but cannot freeze sessions\n",
			style.Warning.Render("!"))
		return nil
	}

	frozen := freezeAllSessions(t, townRoot)

	fmt.Println()
	fmt.Printf("%s %d session(s) frozen\n", style.Error.Render("⛔"), frozen)
	fmt.Printf("   Resume with: %s\n", style.Bold.Render("gt thaw"))

	return nil
}

func runThaw(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	if !estop.IsActive(townRoot) {
		fmt.Println("No E-stop active.")
		return nil
	}

	info := estop.Read(townRoot)

	t := tmux.NewTmux()
	if t.IsAvailable() {
		thawed := thawAllSessions(t, townRoot)
		fmt.Printf("%s %d session(s) resumed\n", style.Success.Render("✓"), thawed)

		// Nudge all sessions to let them know work can continue
		nudged := nudgeAllSessions(t, townRoot)
		if nudged > 0 {
			fmt.Printf("   Nudged %d session(s)\n", nudged)
		}
	}

	// Remove the sentinel file
	if err := estop.Deactivate(townRoot, false); err != nil {
		return fmt.Errorf("failed to remove ESTOP file: %w", err)
	}

	if info != nil {
		duration := time.Since(info.Timestamp).Round(time.Second)
		fmt.Printf("   E-stop was active for %s\n", duration)
	}

	return nil
}

// exemptSessions are sessions that should NOT be frozen during E-stop.
var exemptSessions = map[string]bool{
	session.MayorSessionName():    true,
	session.OverseerSessionName(): true,
}

// freezeAllSessions sends SIGTSTP to all Gas Town agent sessions.
// Mayor and overseer sessions are exempt. Returns count of frozen sessions.
func freezeAllSessions(t *tmux.Tmux, townRoot string) int {
	sessions := collectGTSessions(t, townRoot)
	frozen := 0

	for _, sess := range sessions {
		if exemptSessions[sess] {
			fmt.Printf("   %s %s (exempt)\n", style.Dim.Render("⏭"), sess)
			continue
		}

		if err := signalSession(t, sess, "TSTP"); err != nil {
			fmt.Printf("   %s %s: %v\n", style.Warning.Render("!"), sess, err)
			continue
		}
		fmt.Printf("   %s %s\n", style.Error.Render("⏸"), sess)
		frozen++
	}

	return frozen
}

// thawAllSessions sends SIGCONT to all Gas Town agent sessions.
// Returns count of thawed sessions.
func thawAllSessions(t *tmux.Tmux, townRoot string) int {
	sessions := collectGTSessions(t, townRoot)
	thawed := 0

	for _, sess := range sessions {
		if exemptSessions[sess] {
			continue
		}
		if err := signalSession(t, sess, "CONT"); err != nil {
			continue
		}
		thawed++
	}

	return thawed
}

// nudgeAllSessions sends a nudge to all GT sessions to alert them of resume.
func nudgeAllSessions(t *tmux.Tmux, townRoot string) int {
	sessions := collectGTSessions(t, townRoot)
	nudged := 0

	for _, sess := range sessions {
		if exemptSessions[sess] {
			continue
		}
		if err := t.NudgeSession(sess, "E-stop cleared. Work may resume."); err == nil {
			nudged++
		}
	}

	return nudged
}

// signalSession sends a signal to all processes in a tmux session.
func signalSession(t *tmux.Tmux, sessionName, signal string) error {
	pid, err := t.GetPanePID(sessionName)
	if err != nil {
		return fmt.Errorf("no PID: %w", err)
	}

	// Signal the pane process and its entire process group
	// This catches child processes (node, claude, etc.)
	descendants := getAllSessionDescendants(pid)
	for _, dpid := range descendants {
		_ = exec.Command("kill", "-"+signal, dpid).Run()
	}
	// Signal the pane process itself
	_ = exec.Command("kill", "-"+signal, pid).Run()
	return nil
}

// getAllSessionDescendants returns all descendant PIDs of a process.
func getAllSessionDescendants(pid string) []string {
	out, err := exec.Command("pgrep", "-P", pid).Output()
	if err != nil {
		return nil
	}
	var pids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			pids = append(pids, line)
			// Recurse for grandchildren
			pids = append(pids, getAllSessionDescendants(line)...)
		}
	}
	return pids
}

// collectGTSessions returns all Gas Town tmux sessions.
func collectGTSessions(t *tmux.Tmux, townRoot string) []string {
	allSessions, err := t.ListSessions()
	if err != nil {
		return nil
	}

	rigs := discoverRigs(townRoot)
	prefixes := make(map[string]bool)
	for _, rigName := range rigs {
		prefixes[session.PrefixFor(rigName)] = true
	}

	var gtSessions []string
	for _, sess := range allSessions {
		if isGTSession(sess, prefixes) {
			gtSessions = append(gtSessions, sess)
		}
	}
	return gtSessions
}

// isGTSession checks if a session name belongs to Gas Town.
func isGTSession(name string, rigPrefixes map[string]bool) bool {
	// Town-level sessions (hq-*)
	if strings.HasPrefix(name, session.HQPrefix) {
		return true
	}

	// Rig-level sessions: <prefix>-witness, <prefix>-refinery,
	// <prefix>-crew-<name>, <prefix>-<polecat-name>
	for prefix := range rigPrefixes {
		if strings.HasPrefix(name, prefix+"-") || name == prefix {
			return true
		}
	}

	return false
}

// discoverRigsForEstop finds all rigs — reuses the discoverRigs from up.go
// which is in the same package.
// (discoverRigs is already defined in up.go)

// addEstopToStatus checks for E-stop and prints a banner if active.
// Called from gt status to surface E-stop state.
func addEstopToStatus(townRoot string) {
	if !estop.IsActive(townRoot) {
		return
	}
	info := estop.Read(townRoot)
	if info == nil {
		return
	}
	age := time.Since(info.Timestamp).Round(time.Second)
	fmt.Printf("%s  E-STOP ACTIVE (%s, %s ago", style.Error.Render("⛔"), info.Trigger, age)
	if info.Reason != "" {
		fmt.Printf(": %s", info.Reason)
	}
	fmt.Println(")")
	fmt.Println()
}

// ESTOPBannerPath returns the path and existence of the ESTOP file.
// Exported for use by the daemon heartbeat loop and agent hooks.
func ESTOPBannerPath(townRoot string) (string, bool) {
	p := filepath.Join(townRoot, estop.FileName)
	_, err := os.Stat(p)
	return p, err == nil
}
