package gates

import "github.com/jazz1x/rallish/internal/cycle"

// StandardPipeline is the single source of truth for the canonical cycle gate
// order: Preflight -> Audit -> (repo-local command gates) -> Philosophy ->
// Polish -> Commit. Both the broker and the CLI one-shot delegate here so the
// order and override plumbing cannot drift between the two entry points.
//
// auditCmd overrides AuditGate's default check command; polishTestCmd overrides
// PolishGate's default test command (empty means use the gate default). Each
// entry in localGates becomes a CommandGate inserted immediately after the audit
// gate, preserving their given order. A nil/empty localGates yields the bare
// base pipeline.
//
// This lives in the gates package (not cycle) because gates imports cycle, so
// only here can every concrete gate type be named directly without an import
// cycle.
func StandardPipeline(auditCmd, polishTestCmd string, localGates []string) cycle.Pipeline {
	pipeline := cycle.Pipeline{
		PreflightGate{},
		AuditGate{CmdOverride: auditCmd},
	}
	for _, rawCmd := range localGates {
		pipeline = append(pipeline, CommandGate{RawCmd: rawCmd})
	}
	pipeline = append(pipeline,
		ClaimGate{},
		PhilosophyGate{},
		PolishGate{TestCmdOverride: polishTestCmd},
		CommitGate{},
	)
	return pipeline
}
