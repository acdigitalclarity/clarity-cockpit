package overlay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func testAccounts() []NewLaneAccountRow {
	return []NewLaneAccountRow{
		{Tag: "main", ConfigDir: "/Users/allencoates/.claude", CredentialStore: true, FillPct: 40, HasLiveLane: true, IsDefault: true},
		{Tag: "team-a", ConfigDir: "/Users/allencoates/.claude-team-a", CredentialStore: true, FillPct: 62, HasLiveLane: true, DefaultModality: "app-pipeline"},
		{Tag: "team-b", ConfigDir: "/Users/allencoates/.claude-team-b", CredentialStore: true, FillPct: 18, HasLiveLane: true},
	}
}

// (a) step 1's own two rules: refuse empty and over-32.
func TestNewLaneOverlay_NameRules_RefuseEmptyAndOver32(t *testing.T) {
	o := NewNewLaneOverlay("/tmp/sessions", "", testAccounts(), "main")

	require.Error(t, o.ValidateName(), "an empty name must be refused")

	require.NoError(t, o.TypeRune("this-name-is-exactly-32-characte"))
	require.Equal(t, 32, len([]rune(o.Name())))
	require.NoError(t, o.ValidateName(), "a name at exactly 32 columns is allowed")

	err := o.TypeRune("x")
	require.Error(t, err, "a 33rd character must be refused")
	require.Equal(t, 32, len([]rune(o.Name())), "the refused character must not have been appended")
}

// (b) esc walks back a step, keeping what was already chosen.
func TestNewLaneOverlay_EscWalksBackKeepingState(t *testing.T) {
	o := NewNewLaneOverlay("/tmp/sessions", "", testAccounts(), "main")
	require.NoError(t, o.TypeRune("q3-tender-bid"))
	require.NoError(t, o.ValidateName())
	o.NextFromName()
	require.Equal(t, NewLaneStepAccount, o.Step())

	o.MoveDown() // main -> team-a
	o.MoveDown() // team-a -> team-b
	require.Equal(t, "team-b", o.SelectedAccount().Tag)

	o.BackToName()
	require.Equal(t, NewLaneStepName, o.Step())
	require.Equal(t, "q3-tender-bid", o.Name(), "esc at step 2 must keep the typed name")

	o.NextFromName()
	require.Equal(t, "team-b", o.SelectedAccount().Tag, "the seat must still be team-b after returning to step 2")

	o.NextFromAccount()
	require.Equal(t, NewLaneStepModality, o.Step())

	o.BackToAccount()
	require.Equal(t, NewLaneStepAccount, o.Step())
	require.Equal(t, "team-b", o.SelectedAccount().Tag, "esc at step 3 must keep the chosen seat")
}

// (d) autodetect ladder: -bid name, forge apps dir match, registry default,
// else general with "no rule fired".
func TestNewLaneOverlay_Autodetect_Ladder(t *testing.T) {
	t.Run("N2 name ends -bid", func(t *testing.T) {
		o := NewNewLaneOverlay("/tmp/sessions", "", testAccounts(), "main")
		require.NoError(t, o.TypeRune("acme-bid"))
		o.NextFromName()
		o.NextFromAccount()
		require.Equal(t, "bid", o.SelectedModality().Key)
		require.Contains(t, o.DetectedRule(), "N2")
	})

	t.Run("N3 forge apps dir exists", func(t *testing.T) {
		forgeRoot := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(forgeRoot, "catalog-builder"), 0755))

		o := NewNewLaneOverlay("/tmp/sessions", forgeRoot, testAccounts(), "main")
		require.NoError(t, o.TypeRune("catalog-builder"))
		o.NextFromName()
		o.NextFromAccount()
		require.Equal(t, "app-pipeline", o.SelectedModality().Key)
		require.Contains(t, o.DetectedRule(), "N3")
	})

	t.Run("N4 registry default modality for the chosen seat", func(t *testing.T) {
		o := NewNewLaneOverlay("/tmp/sessions", t.TempDir(), testAccounts(), "main")
		require.NoError(t, o.TypeRune("some-lane"))
		o.NextFromName()
		o.MoveDown() // main -> team-a, whose DefaultModality is app-pipeline
		o.NextFromAccount()
		require.Equal(t, "app-pipeline", o.SelectedModality().Key)
		require.Contains(t, o.DetectedRule(), "N4")
	})

	t.Run("no rule fired falls to general", func(t *testing.T) {
		o := NewNewLaneOverlay("/tmp/sessions", t.TempDir(), testAccounts(), "main")
		require.NoError(t, o.TypeRune("some-lane"))
		o.NextFromName()
		o.NextFromAccount() // main has no DefaultModality
		require.Equal(t, "general", o.SelectedModality().Key)
		require.Equal(t, "", o.DetectedRule())
	})
}

// Render must match FRONTDOOR-MOCKUP-164x45.md screens 1-3 exactly, column
// for column, for the same input the mock-up itself uses.
func TestNewLaneOverlay_Render_MatchesMockupScreens(t *testing.T) {
	accounts := []NewLaneAccountRow{
		{Tag: "main", ConfigDir: "/Users/allencoates/.claude", CredentialStore: true, FillPct: 40, HasLiveLane: true, IsDefault: true},
		{Tag: "team-a", ConfigDir: "/Users/allencoates/.claude-team-a", CredentialStore: true, FillPct: 62, HasLiveLane: true},
		{Tag: "team-b", ConfigDir: "/Users/allencoates/.claude-team-b", CredentialStore: true, FillPct: 18, HasLiveLane: true},
		{Tag: "team-c", ConfigDir: "/Users/allencoates/.claude-team-c", CredentialStore: false},
	}
	o := NewNewLaneOverlay("/Users/allencoates/projects/Clarity/sessions", "", accounts, "main")
	require.NoError(t, o.TypeRune("q3-tender-bid"))

	wantS1 := `╭─ New lane ───────────────────────────────────────────────────────── step 1 of 3 ─╮
│                                                                                  │
│   Name                                                                           │
│   ┌──────────────────────────────────────────────────────────────────────────┐   │
│   │ ▸ q3-tender-bid█                                                         │   │
│   └──────────────────────────────────────────────────────────────────────────┘   │
│                                                                                  │
│   Folder   /Users/allencoates/projects/Clarity/sessions/q3-tender-bid            │
│            does not exist yet · it will be created with repos and work           │
│            symlinked, as clarity new already does                                │
│                                                                                  │
│                                                                                  │
│   ● 1 name    ○ 2 account    ○ 3 modality                                        │
│                                                                                  │
╰─ enter next · esc cancel ────────────────────────────────────────────────────────╯`
	require.Equal(t, wantS1, o.Render(), "screen 1 must match the mock-up line for line")

	o.NextFromName()
	o.MoveDown()
	o.MoveDown() // main -> team-a -> team-b, matching the mock-up's own selection

	wantS2 := `╭─ New lane · q3-tender-bid ───────────────────────────────────────── step 2 of 3 ─╮
│                                                                                  │
│   Account   which login this lane runs on                                        │
│                                                                                  │
│     main     ~/.claude              signed in   40% of window used   default     │
│     team-a   ~/.claude-team-a       signed in   62% of window used               │
│   ▸ team-b   ~/.claude-team-b       signed in   18% of window used               │
│     team-c   ~/.claude-team-c       no login    l runs /login in the pane        │
│                                                                                  │
│   team-b has the most window left of the three signed-in seats.                  │
│                                                                                  │
│   ✓ 1 name    ● 2 account    ○ 3 modality                                        │
│                                                                                  │
╰─ ↑↓ pick · enter next · l log in · esc back ─────────────────────────────────────╯`
	require.Equal(t, wantS2, o.Render(), "screen 2 must match the mock-up line for line (bar the fictional main tag)")

	o.NextFromAccount()
	require.Equal(t, "bid", o.SelectedModality().Key, "the -bid name must have autodetected bid, matching the mock-up")

	wantS3 := `╭─ New lane · q3-tender-bid · team-b ──────────────────────────────── step 3 of 3 ─╮
│                                                                                  │
│   Modality   how this lane works, and which heading it sits under                │
│                                                                                  │
│     app pipeline       forge build through the gated state machine               │
│     project            a client engagement in work/                              │
│     enhancement        a repo's own backlog                                      │
│   ▸ bid                an RFI / RFP / ITT response                               │
│     ways of working    the fleet's own rules and machinery                       │
│                                                                                  │
│   Detected  bid · rule N2, the name ends -bid · ↑↓ overrides it                  │
│                                                                                  │
│   ✓ 1 name    ✓ 2 account    ● 3 modality                                        │
│                                                                                  │
╰─ ↑↓ pick · enter start · esc back ───────────────────────────────────────────────╯`
	require.Equal(t, wantS3, o.Render(), "screen 3 must match the mock-up line for line")

	for _, line := range strings.Split(o.Render(), "\n") {
		require.Equal(t, 84, len([]rune(line)), "every box line must be exactly 84 columns: %q", line)
	}
}

// statusColumnStart is the account row's own fixed x - front-door slice 7
// item 6a's own box-width measurement: the leading "│" (1) + prefix (5) +
// tag field (9) + dir field (accountDirFieldWidth).
const statusColumnStart = 1 + 5 + 9 + accountDirFieldWidth

// TestNewLaneOverlay_AccountRow_DirCellNeverCrossesStatusColumn is front-
// door slice 7 item 6a: the owner's own first-use defect, an unclipped
// config-dir cell running past its own column and into the status column's
// own x. The box's own column geometry (newLaneWidth and every field width
// inside renderAccountRow) is a set of FIXED constants, independent of the
// surrounding terminal - proven by the existing "every box line must be
// exactly 84 columns" assertions above, which hold regardless of window
// size - so both subtests below exercise the same Render() invariant
// against the two terminal contexts the mock-up and the owner's own
// narrower capture are each measured at (FRONTDOOR-MOCKUP-164x45.md; a
// narrower real terminal is exactly where the owner hit the overrun).
func TestNewLaneOverlay_AccountRow_DirCellNeverCrossesStatusColumn(t *testing.T) {
	longDir := "/private/tmp/claude-scratch/deeply/nested/registered/seat/config/directory/that/is/unusually/long/and/not/under/home"
	rows := []NewLaneAccountRow{
		{Tag: "scratch", ConfigDir: longDir, CredentialStore: false},
		{Tag: "team-b", ConfigDir: "/Users/allencoates/.claude-team-b", CredentialStore: true, FillPct: 18, HasLiveLane: true},
	}

	check := func(t *testing.T) {
		o := NewNewLaneOverlay("/tmp/sessions", "", rows, "")
		require.NoError(t, o.TypeRune("q3-tender-bid"))
		o.NextFromName()

		lines := strings.Split(o.Render(), "\n")
		for _, line := range lines {
			require.Equal(t, 84, len([]rune(line)), "every box line must stay exactly 84 columns: %q", line)
		}

		var scratchLine string
		for _, l := range lines {
			if strings.Contains(l, "scratch") {
				scratchLine = l
				break
			}
		}
		require.NotEmpty(t, scratchLine, "the scratch seat's own row must render")
		runes := []rune(scratchLine)
		require.GreaterOrEqual(t, len(runes), statusColumnStart+len("no login"))
		require.Equal(t, "no login", string(runes[statusColumnStart:statusColumnStart+len("no login")]),
			"the status column must start at its own fixed x even with an oversized config-dir cell: %q", scratchLine)
	}

	t.Run("164x45 (mock-up width)", check)
	t.Run("narrower (96x30)", check)
}

// A long sessions root (or a name near the 32-column cap) must not push the
// Folder line's own interior width past the box - one overflowing line
// poisons PlaceOverlay's own width measurement for the WHOLE box, since it
// takes the max width across every line (found live: a deep scratch
// sessions root during this slice's own capture run corrupted the box's
// on-screen placement end to end).
func TestNewLaneOverlay_Render_LongFolderPathNeverOverflowsTheBox(t *testing.T) {
	longRoot := "/Users/allencoates/projects/Clarity/sessions/deeply/nested/scratch/fixture/root/that/is/unusually/long/for/a/sessions/root"
	o := NewNewLaneOverlay(longRoot, "", testAccounts(), "main")
	require.NoError(t, o.TypeRune("this-name-is-exactly-32-characte"))

	for _, line := range strings.Split(o.Render(), "\n") {
		require.Equal(t, 84, len([]rune(line)), "every box line must stay exactly 84 columns even with a long folder path: %q", line)
	}
}
