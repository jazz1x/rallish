package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/spf13/cobra"
)

// cycle_verify.go implements `rallish cycle verify`: the audit surface over a
// cycle's append-only ledger (G4). It is the first production caller of the
// RFC 9162 Merkle library in pkg/contract — it reports the hash-chain integrity
// AND the Merkle tree head, and on request produces + verifies an inclusion or
// consistency proof. rallish computes the verdict client-side over the entries
// the daemon returns; the math does not trust the daemon to self-attest.

type cycleVerifyOptions struct {
	cycleID     string
	inclusion   int // -1 = not requested; else prove entry i is committed
	consistency int // -1 = not requested; else prove first N entries are a prefix
}

// CycleVerifyCmd returns the `cycle verify` subcommand.
func CycleVerifyCmd() *cobra.Command {
	opts := cycleVerifyOptions{inclusion: -1, consistency: -1}
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify a cycle ledger's hash chain and RFC 9162 Merkle proofs",
		Long: `Audit a cycle's append-only ledger (G4):

  - hash-chain integrity (every prev_hash link + recomputed content hash)
  - the RFC 9162 Merkle tree head (root) over all entries
  - optionally, an inclusion proof that a given entry is committed at its index
  - optionally, a consistency proof that the first N entries are an append-only
    prefix of the full log

Exits non-zero if the chain is tampered or a requested proof fails to verify, so
a cron/CI auditor can gate on it.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home dir: %w", err)
			}
			return runCycleVerify(cmd.Context(), home, opts, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&opts.cycleID, "cycle-id", "", "Cycle ID (required)")
	cmd.Flags().IntVar(&opts.inclusion, "inclusion", -1, "Prove entry at this index is committed in the Merkle tree")
	cmd.Flags().IntVar(&opts.consistency, "consistency", -1, "Prove the first N entries are an append-only prefix of the full log")
	_ = cmd.MarkFlagRequired("cycle-id")
	return cmd
}

func runCycleVerify(ctx context.Context, home string, opts cycleVerifyOptions, out io.Writer) error {
	bc, err := resolveBrokerClient(home, 0)
	if err != nil {
		return err
	}
	entries, err := readCycleLedgerWithClient(ctx, bc, opts.cycleID)
	if err != nil {
		return err
	}
	return verifyLedgerEntries(entries, opts, out)
}

// verifyLedgerEntries is the pure verification body, factored out so tests can
// drive it without a broker. It returns a verifyFailedError (non-zero exit) when
// the chain is tampered or a requested proof does not verify.
func verifyLedgerEntries(entries []contract.HarnessLedgerEntry, opts cycleVerifyOptions, out io.Writer) error {
	n := len(entries)
	_, _ = fmt.Fprintf(out, "cycle %s — %s\n", opts.cycleID, count(n, "ledger entry", "ledger entries"))

	failed := false

	// 1. Hash-chain integrity (G4 tamper-evidence).
	if brokenIndex, ok := contract.VerifyChain(entries); ok {
		_, _ = fmt.Fprintf(out, "hash-chain:   ✓ intact\n")
	} else {
		_, _ = fmt.Fprintf(out, "hash-chain:   ✗ TAMPERED at entry %d\n", brokenIndex)
		failed = true
	}

	// 2. RFC 9162 Merkle tree head.
	root := contract.MerkleRoot(entries)
	_, _ = fmt.Fprintf(out, "merkle-root:  %s (RFC 9162)\n", root)

	// 3. Optional inclusion proof.
	if opts.inclusion >= 0 {
		proof, perr := contract.InclusionProof(entries, opts.inclusion)
		switch {
		case perr != nil:
			_, _ = fmt.Fprintf(out, "inclusion[%d]: ✗ %v\n", opts.inclusion, perr)
			failed = true
		case contract.VerifyInclusion(entries[opts.inclusion], proof, root):
			_, _ = fmt.Fprintf(out, "inclusion[%d]: ✓ committed (audit path: %s)\n",
				opts.inclusion, count(len(proof.AuditPath), "node", "nodes"))
		default:
			_, _ = fmt.Fprintf(out, "inclusion[%d]: ✗ proof did not verify against root\n", opts.inclusion)
			failed = true
		}
	}

	// 4. Optional consistency proof: the first N entries vs the full log.
	if opts.consistency >= 0 {
		oldRoot := ""
		if opts.consistency <= n {
			oldRoot = contract.MerkleRoot(entries[:opts.consistency])
		}
		proof, cerr := contract.ConsistencyProof(opts.consistency, entries)
		switch {
		case opts.consistency > n:
			_, _ = fmt.Fprintf(out, "consistency:  ✗ oldSize %d exceeds %d entries\n", opts.consistency, n)
			failed = true
		case cerr != nil:
			_, _ = fmt.Fprintf(out, "consistency:  ✗ %v\n", cerr)
			failed = true
		case contract.VerifyConsistency(proof, oldRoot, root):
			_, _ = fmt.Fprintf(out, "consistency:  ✓ first %d entries are an append-only prefix\n", opts.consistency)
		default:
			_, _ = fmt.Fprintf(out, "consistency:  ✗ proof did not verify (not an append-only prefix)\n")
			failed = true
		}
	}

	if failed {
		return &verifyFailedError{}
	}
	return nil
}

// count renders "N singular" or "N plural" for human-readable output.
func count(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// verifyFailedError carries a non-zero exit code so a cron/CI auditor can gate on
// a tampered ledger or a failed proof, matching the exit-code-carrier pattern the
// gate and cycle-run paths use.
type verifyFailedError struct{}

func (e *verifyFailedError) Error() string {
	return "ledger verification failed (tampered chain or unverifiable proof)"
}

// ExitCode reports a dedicated non-zero code for a failed audit.
func (e *verifyFailedError) ExitCode() int { return 15 }
