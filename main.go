package main

import (
	"claude-squad/app"
	cmd2 "claude-squad/cmd"
	"claude-squad/config"
	"claude-squad/daemon"
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/clarity"
	"claude-squad/session/git"
	"claude-squad/session/tmux"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	version      = "1.0.20"
	programFlag  string
	autoYesFlag  bool
	daemonFlag   bool
	noSplashFlag bool
	binName      string
	rootCmd      = &cobra.Command{
		Use:   "claude-squad",
		Short: "Clarity Workspace - Manage multiple AI agents like Claude Code, Aider, Codex, and Amp.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			log.Initialize(daemonFlag)
			defer log.Close()

			if daemonFlag {
				cfg := config.LoadConfig()
				err := daemon.RunDaemon(cfg)
				log.ErrorLog.Printf("failed to start daemon %v", err)
				return err
			}

			// Check if we're in a git repository
			currentDir, err := filepath.Abs(".")
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}

			if !git.IsGitRepo(currentDir) {
				return fmt.Errorf("error: %s must be run from within a git repository", binName)
			}

			cfg := config.LoadConfig()

			// Program flag overrides config
			program := cfg.GetProgram()
			if programFlag != "" {
				program = programFlag
			}
			// AutoYes flag overrides config
			autoYes := cfg.AutoYes
			if autoYesFlag {
				autoYes = true
			}
			// no-splash flag overrides config - clarity-attach, discover and
			// msg never call app.Run at all, so they never show the splash
			// regardless of this setting.
			noSplash := cfg.NoSplash
			if noSplashFlag {
				noSplash = true
			}
			if autoYes {
				defer func() {
					if err := daemon.LaunchDaemon(); err != nil {
						log.ErrorLog.Printf("failed to launch daemon: %v", err)
					}
				}()
			}
			// Kill any daemon that's running.
			if err := daemon.StopDaemon(); err != nil {
				log.ErrorLog.Printf("failed to stop daemon: %v", err)
			}

			return app.Run(ctx, program, autoYes, noSplash)
		},
	}

	resetCmd = &cobra.Command{
		Use:   "reset",
		Short: "Reset all stored instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Initialize(false)
			defer log.Close()

			state := config.LoadState()
			storage, err := session.NewStorage(state)
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}
			if err := storage.DeleteAllInstances(); err != nil {
				return fmt.Errorf("failed to reset storage: %w", err)
			}
			fmt.Println("Storage has been reset successfully")

			if err := tmux.CleanupSessions(cmd2.MakeExecutor()); err != nil {
				return fmt.Errorf("failed to cleanup tmux sessions: %w", err)
			}
			fmt.Println("Tmux sessions have been cleaned up")

			if err := git.CleanupWorktrees(); err != nil {
				return fmt.Errorf("failed to cleanup worktrees: %w", err)
			}
			fmt.Println("Worktrees have been cleaned up")

			// Kill any daemon that's running.
			if err := daemon.StopDaemon(); err != nil {
				return err
			}
			fmt.Println("daemon has been stopped")

			return nil
		},
	}

	debugCmd = &cobra.Command{
		Use:   "debug",
		Short: "Print debug information like config paths",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Initialize(false)
			defer log.Close()

			cfg := config.LoadConfig()

			configDir, err := config.GetConfigDir()
			if err != nil {
				return fmt.Errorf("failed to get config directory: %w", err)
			}
			configJson, _ := json.MarshalIndent(cfg, "", "  ")

			fmt.Printf("Config: %s\n%s\n", filepath.Join(configDir, config.ConfigFileName), configJson)

			return nil
		},
	}

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print the version number",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("%s version %s\n", binName, version)
			fmt.Printf("https://github.com/acdigitalclarity/clarity-cockpit/releases/tag/v%s\n", version)
		},
	}

	// clarityAttachCmd is a Digital Clarity workspace enhancement (not upstream).
	// A Clarity session lane (sessions/<lane>/) is already an isolated working
	// directory under its own worktree-per-lane discipline. clarity-attach
	// registers a Claude Squad instance pointed straight at that directory -
	// no new git worktree is created, so the lane's own worktrees are
	// untouched - and attaches to it immediately.
	clarityAttachCmd = &cobra.Command{
		Use:   "clarity-attach <lane>",
		Short: "Attach a Clarity Workspace instance to a Clarity session lane's own working directory (no new git worktree)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Initialize(false)
			defer log.Close()

			lane := args[0]
			lanePath, err := clarity.ResolveExistingLaneDir(lane)
			if err != nil {
				return err
			}

			cfg := config.LoadConfig()
			program := cfg.GetProgram()
			if programFlag != "" {
				program = programFlag
			}

			state := config.LoadState()
			storage, err := session.NewStorage(state)
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}

			instances, err := storage.LoadInstances()
			if err != nil {
				return fmt.Errorf("failed to load instances: %w", err)
			}
			for _, existing := range instances {
				if existing.Title == lane {
					return fmt.Errorf("an instance named %q already exists (status %v) - kill it first, or reattach to it from cs", lane, existing.Status)
				}
			}

			inst, err := session.NewInstance(session.InstanceOptions{
				Title:      lane,
				Path:       lanePath,
				Program:    program,
				NoWorktree: true,
			})
			if err != nil {
				return fmt.Errorf("failed to create instance: %w", err)
			}

			if err := inst.Start(true); err != nil {
				return fmt.Errorf("failed to start instance: %w", err)
			}

			instances = append(instances, inst)
			if err := storage.SaveInstances(instances); err != nil {
				return fmt.Errorf("failed to save instance %q: %w", lane, err)
			}

			fmt.Printf("clarity-attach: %q running %s in %s (no git worktree)\n", lane, program, lanePath)
			fmt.Println("Attaching now - press ctrl-q to detach (the instance keeps running; `cs` lists it by lane name).")

			// Put our own stdin/stdout into raw mode for the duration of the
			// attach so keystrokes (including ctrl-q to detach) pass straight
			// through to the tmux session, the same way they do inside the
			// full `cs` TUI (which runs its whole event loop in raw mode).
			if term.IsTerminal(int(os.Stdin.Fd())) {
				oldState, rawErr := term.MakeRaw(int(os.Stdin.Fd()))
				if rawErr != nil {
					log.WarningLog.Printf("could not set stdin to raw mode: %v", rawErr)
				} else {
					defer func() {
						_ = term.Restore(int(os.Stdin.Fd()), oldState)
					}()
				}
			}

			doneCh, err := inst.Attach()
			if err != nil {
				return fmt.Errorf("failed to attach to instance %q: %w", lane, err)
			}
			<-doneCh

			fmt.Printf("\nclarity-attach: detached from %q\n", lane)
			return nil
		},
	}

	// discoverCmd is a Digital Clarity workspace enhancement (not upstream).
	// It shows every LIVE lane on this Mac even when it was started outside
	// the cockpit (a bare Terminal/iTerm tab, or `clarity new`/`clarity open`
	// before cs-clarity existed) - derived the same way
	// scripts/fleet_dashboard.py derives its live-lane list, minus any lane
	// already tracked as a Claude Squad instance. External rows can be
	// messaged (see msgCmd) but never attached or killed - there is no
	// tracked tmux session or git worktree behind them.
	discoverCmd = &cobra.Command{
		Use:   "discover",
		Short: "List every live lane on this Mac not already tracked here, with its context-fill gauge and last write",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Initialize(false)
			defer log.Close()

			state := config.LoadState()
			storage, err := session.NewStorage(state)
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}
			instances, err := storage.LoadInstances()
			if err != nil {
				return fmt.Errorf("failed to load instances: %w", err)
			}
			tracked := make(map[string]bool, len(instances)*2)
			for _, inst := range instances {
				for _, n := range clarity.TrackedExclusionNames(inst.Title) {
					tracked[n] = true
				}
			}

			external, err := clarity.DiscoverExternalLanes(tracked)
			if err != nil {
				return fmt.Errorf("failed to discover external lanes: %w", err)
			}
			if len(external) == 0 {
				fmt.Println("discover: no external lanes live in the last 90 minutes")
				return nil
			}
			for _, ext := range external {
				fillLabel := "n/a"
				if ext.FillOK {
					fillLabel = fmt.Sprintf("%d%%", ext.Fill.Pct)
				}
				fmt.Printf("%-32s ctx %-5s last write %s  (external - message only, cannot attach/kill)\n",
					ext.Name, fillLabel, ext.LastWrite.Format("15:04:05"))
			}
			return nil
		},
	}

	// msgCmd is a Digital Clarity workspace enhancement (not upstream). It
	// delivers text into a lane's live claude prompt by tmux send-keys
	// followed by Enter (session.Instance.SendPrompt does exactly this
	// already), then captures the pane and prints the last line so the
	// caller sees it landed. A lane with no tracked tmux session (an
	// external row - see discoverCmd) gets the fixed UNCONSTRUCTED line
	// instead of a silently dropped or fabricated delivery.
	msgCmd = &cobra.Command{
		Use:   "msg <lane> <text>",
		Short: "Send one line of text into a lane's live prompt, and print back the line that landed",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Initialize(false)
			defer log.Close()

			lane, text := args[0], args[1]

			state := config.LoadState()
			storage, err := session.NewStorage(state)
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}
			instances, err := storage.LoadInstances()
			if err != nil {
				return fmt.Errorf("failed to load instances: %w", err)
			}

			for _, inst := range instances {
				if inst.Title != lane {
					continue
				}
				if !inst.Started() || inst.Paused() || !inst.TmuxAlive() {
					return fmt.Errorf("clarity-squad instance %q is not a live tmux session (started=%v paused=%v)",
						lane, inst.Started(), inst.Paused())
				}
				if err := inst.SendPrompt(text); err != nil {
					return fmt.Errorf("failed to send message to %q: %w", lane, err)
				}
				pane, err := inst.Preview()
				if err != nil {
					return fmt.Errorf("message sent to %q but pane capture failed: %w", lane, err)
				}
				fmt.Println(clarity.LastPaneLine(pane))
				return nil
			}

			// Not a tracked instance - check whether it resolves to a live
			// external lane before giving up.
			external, discErr := clarity.DiscoverExternalLanes(nil)
			if discErr == nil {
				for _, ext := range external {
					if clarity.MatchesQueriedLane(ext, lane) {
						fmt.Println(clarity.ExternalMsgUnconstructed(lane))
						return nil
					}
				}
			}
			return fmt.Errorf("no live lane named %q (checked tracked instances and external transcripts)", lane)
		},
	}
)

func init() {
	rootCmd.Flags().StringVarP(&programFlag, "program", "p", "",
		"Program to run in new instances (e.g. 'aider --model ollama_chat/gemma3:1b')")
	rootCmd.Flags().BoolVarP(&autoYesFlag, "autoyes", "y", false,
		"[experimental] If enabled, all instances will automatically accept prompts")
	rootCmd.Flags().BoolVar(&daemonFlag, "daemon", false, "Run a program that loads all sessions"+
		" and runs autoyes mode on them.")
	rootCmd.Flags().BoolVar(&noSplashFlag, "no-splash", false,
		"Skip the entrance splash screen and start directly in the instance list")

	// Hide the daemonFlag as it's only for internal use
	err := rootCmd.Flags().MarkHidden("daemon")
	if err != nil {
		panic(err)
	}

	clarityAttachCmd.Flags().StringVarP(&programFlag, "program", "p", "",
		"Program to run in the attached instance (e.g. 'aider --model ollama_chat/gemma3:1b')")

	rootCmd.AddCommand(debugCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(resetCmd)
	rootCmd.AddCommand(clarityAttachCmd)
	rootCmd.AddCommand(discoverCmd)
	rootCmd.AddCommand(msgCmd)
}

func main() {
	// Extract the binary name from how this was invoked
	binName = filepath.Base(os.Args[0])
	rootCmd.Use = binName

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
	}
}
