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
	version     = "1.0.20"
	programFlag string
	autoYesFlag bool
	daemonFlag  bool
	binName     string
	rootCmd     = &cobra.Command{
		Use:   "claude-squad",
		Short: "Claude Squad - Manage multiple AI agents like Claude Code, Aider, Codex, and Amp.",
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

			return app.Run(ctx, program, autoYes)
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
			fmt.Printf("https://github.com/smtg-ai/claude-squad/releases/tag/v%s\n", version)
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
		Short: "Attach a Claude Squad instance to a Clarity session lane's own working directory (no new git worktree)",
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
)

func init() {
	rootCmd.Flags().StringVarP(&programFlag, "program", "p", "",
		"Program to run in new instances (e.g. 'aider --model ollama_chat/gemma3:1b')")
	rootCmd.Flags().BoolVarP(&autoYesFlag, "autoyes", "y", false,
		"[experimental] If enabled, all instances will automatically accept prompts")
	rootCmd.Flags().BoolVar(&daemonFlag, "daemon", false, "Run a program that loads all sessions"+
		" and runs autoyes mode on them.")

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
}

func main() {
	// Extract the binary name from how this was invoked
	binName = filepath.Base(os.Args[0])
	rootCmd.Use = binName

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
	}
}
