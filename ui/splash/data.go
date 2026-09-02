package splash

import "claude-squad/session/clarity"

// FleetCounts reads the same two sources ui.List already reads for its own
// "Needs you" feed and external-lane rows (app.go's feedTickMsg handler),
// so the splash's fleet numbers never disagree with the list they hand off
// to: every row of the ranked queue counts toward "needs you" (not just
// the top N the list's feed panel shows), and every discovered external
// lane counts toward "lanes live". Exported so the Session pane's own
// resting frame (ui/session.go, design/cockpit-pane/DECISIONS.md slice 3)
// reads the identical definition rather than re-deriving it.
func FleetCounts() (liveLanes, needsYou int) {
	if items, err := clarity.LoadFeed(clarity.DefaultFeedPath()); err == nil {
		needsYou = len(items)
	}
	if external, err := clarity.DiscoverExternalLanes(nil); err == nil {
		liveLanes = len(external)
	}
	return liveLanes, needsYou
}
