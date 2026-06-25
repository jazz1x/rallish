package session

import (
	"fmt"
	"slices"
)

// DryRounds reports whether the session has at least threshold consecutive dry
// turns at the end of its record history.
//
// A turn is dry when it introduces no new artifacts, does not mark Done, and
// does not request an explicit HandoffTo.
func DryRounds(records []TurnRecord, threshold int) bool {
	if threshold <= 0 {
		return false
	}
	seenArtifacts := make(map[string]struct{})
	consecutive := 0
	for _, rec := range records {
		newArtifacts := collectNewArtifacts(seenArtifacts, rec.Resp.Artifacts)
		if isDry(rec, newArtifacts) {
			consecutive++
		} else {
			consecutive = 0
		}
	}
	return consecutive >= threshold
}

// Stuck detects non-productive oscillation patterns over a session's turn
// records. It returns a reason and true when a stuck pattern is found.
func Stuck(records []TurnRecord) (string, bool) {
	if len(records) == 0 {
		return "", false
	}

	if reason, ok := detectPingPong(records); ok {
		return reason, true
	}

	if reason, ok := detectNoProgress(records); ok {
		return reason, true
	}

	if reason, ok := detectRepeatedFingerprint(records); ok {
		return reason, true
	}

	return "", false
}

func detectNoProgress(records []TurnRecord) (string, bool) {
	const noProgressWindow = 6
	if len(records) < noProgressWindow {
		return "", false
	}
	seenArtifacts := make(map[string]struct{})
	dryInWindow := 0
	for _, rec := range records {
		newArtifacts := collectNewArtifacts(seenArtifacts, rec.Resp.Artifacts)
		if isDry(rec, newArtifacts) {
			dryInWindow++
		} else {
			dryInWindow = 0
		}
	}
	if dryInWindow >= noProgressWindow {
		return "no progress", true
	}
	return "", false
}

func isDry(rec TurnRecord, newArtifacts []string) bool {
	return len(newArtifacts) == 0 && !rec.Resp.Done && rec.Resp.HandoffTo == ""
}

func collectNewArtifacts(seen map[string]struct{}, artifacts []string) []string {
	var newArtifacts []string
	for _, a := range artifacts {
		if _, ok := seen[a]; !ok {
			newArtifacts = append(newArtifacts, a)
			seen[a] = struct{}{}
		}
	}
	return newArtifacts
}

func detectPingPong(records []TurnRecord) (string, bool) {
	if len(records) < 6 {
		return "", false
	}

	// Window scan: look for any 6-turn window of alternating roles, all dry.
	for start := 0; start <= len(records)-6; start++ {
		window := records[start : start+6]
		roles := make([]string, len(window))
		for i, rec := range window {
			roles[i] = rec.Req.Role
		}

		// Alternating A,B,A,B,A,B.
		if roles[0] == roles[2] && roles[2] == roles[4] &&
			roles[1] == roles[3] && roles[3] == roles[5] &&
			roles[0] != roles[1] {
			seen := make(map[string]struct{})
			allDry := true
			for _, rec := range window {
				newArtifacts := collectNewArtifacts(seen, rec.Resp.Artifacts)
				if !isDry(rec, newArtifacts) {
					allDry = false
					break
				}
			}
			if allDry {
				return fmt.Sprintf("ping-pong %s/%s", roles[0], roles[1]), true
			}
		}
	}
	return "", false
}

func detectRepeatedFingerprint(records []TurnRecord) (string, bool) {
	counts := make(map[string]int)
	for _, rec := range records {
		key := fingerprint(rec)
		counts[key]++
		if counts[key] >= 4 {
			return "repeated fingerprint", true
		}
	}
	return "", false
}

func fingerprint(rec TurnRecord) string {
	arts := make([]string, len(rec.Resp.Artifacts))
	copy(arts, rec.Resp.Artifacts)
	slices.Sort(arts)
	return fmt.Sprintf("%s|%q", rec.Resp.Summary, arts)
}
