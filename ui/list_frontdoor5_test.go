package ui

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/session"
	"claude-squad/session/clarity"
	"claude-squad/session/tmux"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// frontdoor5ListWidth164/120 are the LIST component's own column share at a
// 164- and a 120-column TERMINAL (app.go's listWidthForTerminal: round(164
// * 0.28) = 46, round(120 * 0.28) = 34 clamped up to listWidthMin 38) -
// List.SetSize takes the component's own width, never the whole terminal's
// (every other test in this package already calls it this way), so a test
// simulating "FRONTDOOR-MOCKUP-164x45.md's own 164-wide screen" must hand
// the list its own 46-column share, not 164 itself.
const (
	frontdoor5ListWidth164 = 46
	frontdoor5ListWidth120 = 38
)

// frontdoor5Time builds a fixed local HH:MM the same way a real transcript's
// last-turn read does (Local().Format("15:04")), so a row's own time field
// is deterministic regardless of the machine's timezone or the moment the
// suite runs - the date itself is arbitrary.
func frontdoor5Time(hh, mm int) time.Time {
	return time.Date(2026, 9, 3, hh, mm, 0, 0, time.Local)
}

// frontdoor5Instance builds a tracked instance carrying an account, a
// modality and a resolved context-fill/state reading - the three fields
// slice 5's grouped list, tag column and fleet line all key on. Alive
// (slice 17b): a fake tmux session whose Run always succeeds, so
// TmuxAlive() reads true - the same SetTmuxSession/NewTmuxSessionWithDeps
// seam composer_test.go already uses for a "live tmux" fixture - since
// this helper's own tests are exercising state/sort/attention behaviour
// unrelated to liveness, every existing caller keeps reading exactly the
// alive lane it always implicitly was before liveness existed as a
// concept; TestFrontdoor5* callers that specifically want a DEAD row build
// one directly instead (see list_liveness_test.go).
func frontdoor5Instance(t *testing.T, title, account, modality string, pct int, state string, when time.Time) *session.Instance {
	t.Helper()
	inst, err := session.NewInstance(session.InstanceOptions{
		Title:    title,
		Path:     ".",
		Program:  "echo",
		Account:  account,
		Modality: modality,
	})
	require.NoError(t, err)
	inst.SetTmuxSession(tmux.NewTmuxSessionWithDeps(title, "echo", nil, cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error { return nil },
	}))
	inst.SetContextFill(pct, true)
	inst.SetLaneState(state, when, true)
	return inst
}

// frontdoor5External builds an external lane with the same three fields.
// Alive defaults true (slice 17b) for the same "already implicitly alive"
// reason frontdoor5Instance's own doc comment gives.
func frontdoor5External(name, account, seatSource, modality string, pct int, state string, when time.Time) clarity.ExternalLane {
	return clarity.ExternalLane{
		Name:       name,
		Account:    account,
		SeatSource: seatSource,
		Modality:   modality,
		Fill:       clarity.Fill{Pct: pct},
		FillOK:     true,
		State:      state,
		LastTurn:   when,
		StateOK:    true,
		LastWrite:  when,
		Alive:      true,
	}
}

// --- (a) groups render in the stated order, only when non-empty ----------

// TestGroupLanesByModality_FirstSeenOrder is groupLanesByModality's own unit
// proof: groups appear in the order their modality is FIRST seen scanning
// tracked instances then external lanes, never a fixed canonical list - see
// the function's own doc comment for why (FRONTDOOR-MOCKUP-164x45.md screen
// 4's own heading order does not follow the brief's literal "app pipeline,
// project, bid, enhancement, general" text either; the drawing is the bar,
// and insertion order is what actually reproduces it - this leg's report
// names the discrepancy).
func TestGroupLanesByModality_FirstSeenOrder(t *testing.T) {
	items := []*session.Instance{
		frontdoor5Instance(t, "bid-lane", "tb", "bid", 10, clarity.StateWorking, frontdoor5Time(9, 0)),
		frontdoor5Instance(t, "pipeline-lane", "ta", "app pipeline", 20, clarity.StateIdle, frontdoor5Time(9, 1)),
		frontdoor5Instance(t, "project-lane", "m1", "project", 30, clarity.StateIdle, frontdoor5Time(9, 2)),
		frontdoor5Instance(t, "no-modality-lane", "ta", "", 5, clarity.StateIdle, frontdoor5Time(9, 3)),
	}
	groups := groupLanesByModality(items, nil)
	require.Len(t, groups, 4, "three named groups plus the trailing no-modality catch-all")
	require.Equal(t, "bid", groups[0].modality)
	require.Equal(t, "app pipeline", groups[1].modality)
	require.Equal(t, "project", groups[2].modality)
	require.Equal(t, "", groups[3].modality, "the trailing element is always the no-modality catch-all")
	require.Equal(t, []int{3}, groups[3].itemIdx)
}

// TestString_ModalityHeadings_OnlyWhenNonEmpty is the same rule through the
// real render path: a heading appears for every modality actually present,
// in first-seen order, and never for one with zero rows.
func TestString_ModalityHeadings_OnlyWhenNonEmpty(t *testing.T) {
	l := newTestList()
	l.SetSize(frontdoor5ListWidth164, 45)
	l.AddInstance(frontdoor5Instance(t, "bid-lane", "tb", "bid", 10, clarity.StateWorking, frontdoor5Time(9, 0)))
	l.AddInstance(frontdoor5Instance(t, "pipeline-lane", "ta", "app pipeline", 20, clarity.StateIdle, frontdoor5Time(9, 1)))

	out := ansi.Strip(l.String())
	require.Contains(t, out, " Bid ")
	require.Contains(t, out, " App pipeline ")
	require.Less(t, strings.Index(out, " Bid "), strings.Index(out, " App pipeline "),
		"headings render in first-seen order")
	require.NotContains(t, out, " Project ", "a heading never appears for a modality with zero rows")
	require.NotContains(t, out, " Enhancement ")
}

// --- (b) the tag column renders and drops at 120 wide ---------------------

// TestLaneShowTag_DropsBelow120 pins the threshold itself: the tag column
// shows at the mock's own 164-column width and is gone at its own
// 120-column width (FRONTDOOR-MOCKUP-164x45.md's two width variants).
func TestLaneShowTag_DropsBelow120(t *testing.T) {
	l := newTestList()
	l.AddInstance(frontdoor5Instance(t, "a", "tb", "", 10, clarity.StateWorking, frontdoor5Time(9, 0)))

	l.SetSize(frontdoor5ListWidth164, 45)
	require.True(t, laneShowTag(l.rowInnerWidth()), "the mock's own 164-wide screen carries the tag column")
	rowLine164 := rowLineContaining(t, l.String(), "1. a")
	require.Contains(t, rowLine164, "tb", "the row's own tag must render at 164 columns")

	l.SetSize(frontdoor5ListWidth120, 36)
	require.False(t, laneShowTag(l.rowInnerWidth()), "the mock's own 120-wide screen carries no tag column")
	rowLine120 := rowLineContaining(t, l.String(), "1. a")
	require.NotContains(t, rowLine120, "tb", "the tag column must be gone entirely at 120 columns")
}

// rowLineContaining returns the single stripped line of s that contains
// needle, failing the test if none (or more than one, since every case this
// helper is used for expects exactly one match) does.
func rowLineContaining(t *testing.T, s, needle string) string {
	t.Helper()
	var found string
	n := 0
	for _, line := range strings.Split(ansi.Strip(s), "\n") {
		if strings.Contains(line, needle) {
			found = line
			n++
		}
	}
	require.Equal(t, 1, n, "expected exactly one line containing %q: %q", needle, s)
	return found
}

// --- (c) "default folder" rows carry no tag --------------------------------

// TestExternalRowTag_DefaultFolderHasNoTag is externalRowTag's own unit
// proof (slice 5 item 2): a declared seat, "desktop", and a non-default
// folder-login seat all show the bare tag; the unlogged-in DEFAULT floor
// shows none at all. The row never shows SeatTag's own "<tag> <source>"
// bracket text - that belongs to lane-tail/discover, not this row.
func TestExternalRowTag_DefaultFolderHasNoTag(t *testing.T) {
	require.Equal(t, "", externalRowTag(clarity.ExternalLane{Account: "default", SeatSource: clarity.SeatSourceFolder}))
	require.Equal(t, "tb", externalRowTag(clarity.ExternalLane{Account: "tb", SeatSource: clarity.SeatSourceDeclared}))
	require.Equal(t, "desktop", externalRowTag(clarity.ExternalLane{Account: "desktop", SeatSource: clarity.SeatSourceDesktop}))
	require.Equal(t, "team-a", externalRowTag(clarity.ExternalLane{Account: "team-a", SeatSource: clarity.SeatSourceFolderLogin}),
		"the row shows the bare seat, never lane-tail/discover's own '<tag> <source>' bracket")
}

// TestString_DefaultFolderExternalRow_NoTagText is the same rule through
// the real render path: a "default folder" external row's own line carries
// no tag text at all, even though the tag column itself is showing.
func TestString_DefaultFolderExternalRow_NoTagText(t *testing.T) {
	l := newTestList()
	l.SetSize(frontdoor5ListWidth164, 45)
	l.SetExternal([]clarity.ExternalLane{
		{Name: "legacy-lane", Account: "default", SeatSource: clarity.SeatSourceFolder,
			Fill: clarity.Fill{Pct: 5}, FillOK: true, LastWrite: frontdoor5Time(9, 0)},
	})

	rowLine := rowLineContaining(t, l.String(), "legacy-lane")
	require.NotContains(t, rowLine, "default", "a default-folder row shows no tag text at all")
}

// --- (d) the fleet line takes the max per seat, omits an empty seat -------

// TestFleetLine_MaxPerSeat_OmitsEmptySeat is slice 5 item 3's own proof:
// over a fixture of two seats with two lanes each, the figure is the
// MAXIMUM of that seat's own lanes, never their sum (research F7 - "the
// harness sums nothing") - and a third, registered but lane-less seat is
// omitted entirely rather than shown at 0%.
func TestFleetLine_MaxPerSeat_OmitsEmptySeat(t *testing.T) {
	l := newTestList()
	l.AddInstance(frontdoor5Instance(t, "a1", "ta", "", 40, clarity.StateWorking, frontdoor5Time(9, 0)))
	l.AddInstance(frontdoor5Instance(t, "a2", "ta", "", 62, clarity.StateIdle, frontdoor5Time(9, 1)))
	l.SetExternal([]clarity.ExternalLane{
		frontdoor5External("b1", "tb", clarity.SeatSourceDeclared, "", 18, clarity.StateIdle, frontdoor5Time(9, 2)),
		frontdoor5External("b2", "tb", clarity.SeatSourceDeclared, "", 5, clarity.StateIdle, frontdoor5Time(9, 3)),
	})
	l.SetAccountsRegistry(map[string]string{"ta": "/x", "tb": "/y", "empty-seat": "/z"})

	require.Equal(t, "ta 62% · tb 18%", l.fleetLine(),
		"the sum would be 102/23 - the MAXIMUM per seat is 62/18, and empty-seat never appears")
}

// TestFleetLine_EmptyWhenNoRegistry proves the line is dropped entirely
// (never rendered blank) when SetAccountsRegistry has never been called -
// today's every other test's own backward-compatibility floor.
func TestFleetLine_EmptyWhenNoRegistry(t *testing.T) {
	l := newTestList("a")
	require.Equal(t, "", l.fleetLine())
}

// TestString_FleetLine_SitsDirectlyUnderTitle is slice 5 item 3's own
// placement proof: no blank line between "Instances" and the fleet line, or
// between the fleet line and "Needs you" - FRONTDOOR-MOCKUP-164x45.md
// screen 4's own rows 4-6.
func TestString_FleetLine_SitsDirectlyUnderTitle(t *testing.T) {
	l := newTestList("a")
	l.SetSize(frontdoor5ListWidth164, 45)
	l.items[0].SetAccount("ta")
	l.items[0].SetContextFill(62, true)
	l.SetAccountsRegistry(map[string]string{"ta": "/x"})
	l.SetNeedsYou(testNeedsYouItems("#277"), "")

	lines := strings.Split(ansi.Strip(l.String()), "\n")
	var titleIdx, fleetIdx, needsYouIdx = -1, -1, -1
	for i, line := range lines {
		if strings.Contains(line, "Instances") && titleIdx == -1 {
			titleIdx = i
		}
		if strings.Contains(line, "ta 62%") {
			fleetIdx = i
		}
		if strings.Contains(line, "Needs you") && needsYouIdx == -1 {
			needsYouIdx = i
		}
	}
	require.NotEqual(t, -1, titleIdx)
	require.Equal(t, titleIdx+1, fleetIdx, "the fleet line sits directly under the title, no blank line")
	require.Equal(t, fleetIdx+1, needsYouIdx, "Needs you sits directly under the fleet line, no blank line")
}

// --- (e) a 164x45 render matches screen 4 ----------------------------------

// TestString_MatchesScreen4_164x45 builds a fixture in the same shape as
// FRONTDOOR-MOCKUP-164x45.md screen 4 (same lane names, accounts,
// modalities, percentages, states and times) and compares the render
// against it: heading order, the fleet-line/title/Needs-you adjacency, and
// - since this fixture's data matches the mock's own row for row - two
// tracked rows byte-for-byte against the mock's own text (this leg's report
// derives the tag-column arithmetic from the mock's own byte positions).
// Group ORDER matches the mock's own drawn order (Ways of working, App
// pipeline, Enhancement, Project, Bid) because groupLanesByModality's own
// first-seen rule reproduces it from this fixture's insertion order - see
// that function's doc comment on why this, and not the brief's own literal
// "app pipeline, project, bid, enhancement, general" text, is what the
// drawing actually does.
func TestString_MatchesScreen4_164x45(t *testing.T) {
	l := newTestList()
	l.AddInstance(frontdoor5Instance(t, "ways-of-working", "ta", "ways of working", 62, clarity.StateWorking, frontdoor5Time(11, 41)))
	l.AddInstance(frontdoor5Instance(t, "cmpro-milestone-1", "tb", "app pipeline", 8, clarity.StateWorking, frontdoor5Time(13, 2)))
	l.AddInstance(frontdoor5Instance(t, "cockpit", "ta", "enhancement", 15, clarity.StateWaitingYou, frontdoor5Time(11, 41)))
	l.AddInstance(frontdoor5Instance(t, "cockpit2", "tb", "enhancement", 13, clarity.StateWaitingYou, frontdoor5Time(11, 26)))
	l.AddInstance(frontdoor5Instance(t, "fastned", "m1", "project", 36, clarity.StateWaitingYou, frontdoor5Time(11, 36)))
	l.AddInstance(frontdoor5Instance(t, "q3-tender-bid", "tb", "bid", 2, clarity.StateWorking, frontdoor5Time(13, 7)))

	l.SetExternal([]clarity.ExternalLane{
		frontdoor5External("weekend-run", "ta", clarity.SeatSourceDeclared, "ways of working", 34, clarity.StateIdle, frontdoor5Time(13, 43)),
		frontdoor5External("travel-matrix-m4", "tb", clarity.SeatSourceDeclared, "app pipeline", 23, clarity.StateWorking, frontdoor5Time(11, 40)),
		frontdoor5External("audit-v2-build", "tb", clarity.SeatSourceDeclared, "app pipeline", 50, clarity.StateIdle, frontdoor5Time(13, 42)),
		frontdoor5External("mcp-and-ideation", "m1", clarity.SeatSourceDeclared, "enhancement", 53, clarity.StateIdle, frontdoor5Time(20, 26)),
		frontdoor5External("dubai-opportunity", "m1", clarity.SeatSourceDeclared, "project", 23, clarity.StateStalled, frontdoor5Time(22, 11)),
		frontdoor5External("andy-e-bid", "m1", clarity.SeatSourceDeclared, "bid", 50, clarity.StateIdle, frontdoor5Time(7, 34)),
	})

	l.SetAccountsRegistry(map[string]string{"ta": "/x", "tb": "/y", "m1": "/z"})
	l.SetNeedsYou([]clarity.FeedItem{
		{Source: "board#277", Title: "Owner: one settings act - move stat"},
		{Source: "board#244", Title: "Read the Clarity Beta options paper"},
	}, "")
	l.SetSelectedInstance(5) // q3-tender-bid - the lane screen 4's own scenario just started
	l.SetSize(frontdoor5ListWidth164, 45)

	stripped := ansi.Strip(l.String())
	lines := strings.Split(stripped, "\n")

	// Headings, in first-seen order - screen 4's own order exactly.
	headingOrder := []string{" Ways of working ", " App pipeline ", " Enhancement ", " Project ", " Bid "}
	last := -1
	for _, h := range headingOrder {
		idx := strings.Index(stripped, h)
		require.GreaterOrEqualf(t, idx, 0, "heading %q must appear: %q", h, stripped)
		require.Greaterf(t, idx, last, "headings must render in first-seen (screen 4's own) order: %q out of place", h)
		last = idx
	}

	// Fleet line: ta 62% (max of 62/34), tb 50% (max of 8/23/50/13/2), m1
	// 53% (max of 36/53/23/50) - sorted by tag (m1 < ta < tb), sitting
	// directly under "Instances", before "Needs you", exactly as screen 4's
	// own rows 4-6.
	require.Equal(t, "m1 53% · ta 62% · tb 50%", l.fleetLine())
	var titleIdx, fleetIdx, needsYouIdx = -1, -1, -1
	for i, line := range lines {
		if strings.Contains(line, "Instances") && titleIdx == -1 {
			titleIdx = i
		}
		if strings.Contains(line, "ta 62%") {
			fleetIdx = i
		}
		if strings.Contains(line, "Needs you") && needsYouIdx == -1 {
			needsYouIdx = i
		}
	}
	require.Equal(t, titleIdx+1, fleetIdx, "the fleet line sits directly under the title")
	require.Equal(t, fleetIdx+1, needsYouIdx, "Needs you sits directly under the fleet line")

	// Two tracked rows, byte-for-byte against the mock's own text (marker +
	// content + trailing pad; the mock's own border character belongs to a
	// different component, not this one) - a two-digit and a one-digit
	// percentage, proving the tag/pct/gap arithmetic both ways.
	require.Contains(t, stripped, "1. ways-of-working   ta  62% ● working 11:41",
		"screen 4's own row 1, byte for byte")
	require.Contains(t, stripped, "6. q3-tender-bid     tb   2% ● working 13:07",
		"screen 4's own selected row 6, byte for byte")
	require.True(t, strings.HasPrefix(rowLineContaining(t, stripped, "q3-tender-bid"), "▌"),
		"the just-started lane is the current selection, marker included")
}
