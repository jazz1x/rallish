package contract

import (
	"errors"
	"strings"
	"testing"
)

func TestActionDecisionForDenies(t *testing.T) {
	cases := []struct {
		name    string
		command string
		rule    string
	}{
		{"rm-rf-slash", "rm -rf /", "rm-rf-root"},
		{"rm-rf-slash-glob", "rm -rf /*", "rm-rf-root"},
		{"rm-rf-home-tilde", "rm -rf ~", "rm-rf-root"},
		{"rm-rf-home-glob", "rm -rf ~/*", "rm-rf-root"},
		{"rm-rf-home-var", "rm -rf $HOME/", "rm-rf-root"},
		{"rm-fr-combined", "rm -fr /", "rm-rf-root"},
		{"rm-split-flags", "rm --recursive --force /", "rm-rf-root"},
		{"rm-rf-tabs", "rm\t-rf\t/", "rm-rf-root"},
		{"fork-bomb-canonical", ":(){ :|:& };:", "fork-bomb"},
		{"fork-bomb-spaced", ":(){ : | : & } ; :", "fork-bomb"},
		{"dd-to-disk", "dd if=/dev/zero of=/dev/sda bs=1M", "disk-overwrite"},
		{"mkfs-disk", "mkfs.ext4 /dev/sda1", "disk-overwrite"},
		{"redirect-disk", "cat junk > /dev/sda", "disk-overwrite"},
		{"force-push-main", "git push --force origin main", "git-force-push-protected"},
		{"force-push-master-shortflag", "git push -f origin master", "git-force-push-protected"},
		{"force-push-release", "git push --force origin release/1.2", "git-force-push-protected"},
		{"force-with-lease-main", "git push --force-with-lease origin main", "git-force-push-protected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ActionDecisionFor(tc.command)
			if got.Verdict != ActionDeny {
				t.Fatalf("verdict = %q, want deny (rule %s); reason=%q", got.Verdict, tc.rule, got.Reason)
			}
			if got.Rule != tc.rule {
				t.Fatalf("rule = %q, want %q", got.Rule, tc.rule)
			}
			if got.Reason == "" {
				t.Fatal("deny decision must carry a reason")
			}
		})
	}
}

func TestActionDecisionForNeedsHITL(t *testing.T) {
	cases := []struct {
		name    string
		command string
		rule    string
	}{
		{"reset-hard-origin", "git reset --hard origin/main", "git-hard-reset-remote"},
		{"reset-hard-upstream", "git reset --hard upstream/master", "git-hard-reset-remote"},
		{"drop-table", "psql -c 'DROP TABLE users'", "sql-drop-truncate"},
		{"drop-database", "mysql -e 'DROP DATABASE prod'", "sql-drop-truncate"},
		{"truncate-table", "psql -c 'TRUNCATE TABLE orders'", "sql-drop-truncate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ActionDecisionFor(tc.command)
			if got.Verdict != ActionNeedsHITL {
				t.Fatalf("verdict = %q, want needs-hitl (rule %s)", got.Verdict, tc.rule)
			}
			if got.Rule != tc.rule {
				t.Fatalf("rule = %q, want %q", got.Rule, tc.rule)
			}
		})
	}
}

// TestActionDecisionForAllowsSafeCommands is the false-positive guard: a decision
// layer that blocks legitimate work is worse than one that misses (rallish is not
// the enforcer). These commands MUST all be allowed.
func TestActionDecisionForAllowsSafeCommands(t *testing.T) {
	safe := []string{
		"",
		"ls -la",
		"go test ./...",
		"git status",
		"git push origin feat/autonomous-cycle", // feature-branch push
		"git push --force origin feat/autonomous-cycle", // force-push to a feature branch is fine
		"git push -f origin my-topic-branch",            // ditto, short flag
		"rm -rf ./build",                                // scoped relative dir
		"rm -rf node_modules",                           // named dir
		"rm -rf ./dist/*",                               // scoped glob
		"rm -rf tmp/cache",                              // nested relative
		"rm file.txt",                                   // non-recursive
		"git reset --hard HEAD",                         // local reset, not a remote ref
		"git reset --soft origin/main",                  // soft reset is not destructive
		"dd if=input.bin of=output.bin",                 // file-to-file dd, not a device
		"echo 'DROP the beat'",                          // substring not a SQL statement context we match
		"docker rm -f mycontainer",                      // not an rm of a path root
		"truncate -s 0 logfile.txt",                     // file truncate, allowed (only TABLE/leading truncate of db)
	}
	for _, cmd := range safe {
		t.Run(strings.ReplaceAll(cmd, " ", "_"), func(t *testing.T) {
			got := ActionDecisionFor(cmd)
			if got.Verdict != ActionAllow {
				t.Fatalf("command %q: verdict = %q rule=%q, want allow (false positive)", cmd, got.Verdict, got.Rule)
			}
		})
	}
}

func TestParseActionVerdict(t *testing.T) {
	for _, s := range []string{"allow", "deny", "needs-hitl"} {
		if _, err := ParseActionVerdict(s); err != nil {
			t.Fatalf("ParseActionVerdict(%q) unexpected error: %v", s, err)
		}
	}
	if _, err := ParseActionVerdict("nope"); !errors.Is(err, ErrInvalidActionVerdict) {
		t.Fatalf("ParseActionVerdict(nope) error = %v, want ErrInvalidActionVerdict", err)
	}
}

func TestActionVerdictString(t *testing.T) {
	if ActionDeny.String() != "deny" {
		t.Fatalf("String() = %q, want deny", ActionDeny.String())
	}
}

func TestNewActionDeniedLedgerEntry(t *testing.T) {
	decision := ActionDecisionFor("rm -rf /")
	entry := NewActionDeniedLedgerEntry(1700, "cyc_g6", "rm -rf /", decision)

	if entry.Type != LedgerEventActionDenied {
		t.Fatalf("type = %q, want %q", entry.Type, LedgerEventActionDenied)
	}
	if entry.At != 1700 || entry.CycleID != "cyc_g6" {
		t.Fatalf("at/cycle = %d/%q", entry.At, entry.CycleID)
	}
	if entry.SchemaVersion != LedgerSchemaVersion {
		t.Fatalf("schema_version = %q, want %q", entry.SchemaVersion, LedgerSchemaVersion)
	}
	if !strings.Contains(entry.Summary, "deny") || !strings.Contains(entry.Summary, "rm-rf-root") {
		t.Fatalf("summary = %q, want verdict+rule", entry.Summary)
	}
	if len(entry.Files) != 1 || entry.Files[0] != "rm -rf /" {
		t.Fatalf("files = %v, want the recorded command", entry.Files)
	}
}
