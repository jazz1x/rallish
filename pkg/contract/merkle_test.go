package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// makeEntries builds n chained ledger entries with populated Hash fields, exactly
// as the on-write path produces them, so the Merkle leaves reuse real ChainHash
// digests (not a parallel scheme). Returns a fully linked, VerifyChain-valid slice.
func makeEntries(t *testing.T, n int) []HarnessLedgerEntry {
	t.Helper()
	entries := make([]HarnessLedgerEntry, 0, n)
	prev := LedgerGenesisHash
	for i := 0; i < n; i++ {
		e := NewHarnessLedgerEntry(int64(1000+i), "cyc-1", LedgerEventAgentTurn, "turn summary", []string{"file.go"})
		e.PrevHash = prev
		h, err := ChainHash(e, prev)
		if err != nil {
			t.Fatalf("ChainHash(%d): %v", i, err)
		}
		e.Hash = h
		entries = append(entries, e)
		prev = h
	}
	if _, ok := VerifyChain(entries); !ok {
		t.Fatalf("makeEntries produced a chain that fails VerifyChain")
	}
	return entries
}

// --- MerkleRoot: determinism + empty-log root ---

func TestMerkleRootEmptyLogIsDefinedConstant(t *testing.T) {
	// RFC 6962 §2.1: MTH({}) = SHA-256(). Pin to the named constant AND to an
	// independent SHA-256("") computation so the constant itself is verified.
	got := MerkleRoot(nil)
	if got != EmptyMerkleRoot {
		t.Fatalf("empty-log root = %q, want EmptyMerkleRoot %q", got, EmptyMerkleRoot)
	}
	sum := sha256.Sum256([]byte{})
	if want := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("EmptyMerkleRoot %q != independent SHA-256(\"\") %q", got, want)
	}
	// Empty slice (not just nil) must match too.
	if MerkleRoot([]HarnessLedgerEntry{}) != EmptyMerkleRoot {
		t.Fatalf("empty (non-nil) slice root != EmptyMerkleRoot")
	}
}

func TestMerkleRootDeterministic(t *testing.T) {
	entries := makeEntries(t, 7)
	r1 := MerkleRoot(entries)
	r2 := MerkleRoot(entries)
	if r1 != r2 {
		t.Fatalf("MerkleRoot not deterministic: %q vs %q", r1, r2)
	}
	if !isHexSize(r1) {
		t.Fatalf("root %q is not a 32-byte hex digest", r1)
	}
	// A copy with identical content yields the identical root.
	cp := append([]HarnessLedgerEntry(nil), entries...)
	if MerkleRoot(cp) != r1 {
		t.Fatalf("identical-content copy produced a different root")
	}
}

func TestMerkleRootSingleLeafEqualsLeafHash(t *testing.T) {
	// RFC 6962: MTH({d0}) = SHA-256(0x00 || d0). For a single entry the root must be
	// exactly the domain-separated leaf hash over the entry's ChainHash digest.
	entries := makeEntries(t, 1)
	leafData, err := hex.DecodeString(entries[0].Hash)
	if err != nil {
		t.Fatalf("decode entry hash: %v", err)
	}
	h := sha256.New()
	h.Write([]byte{0x00})
	h.Write(leafData)
	want := hex.EncodeToString(h.Sum(nil))
	if got := MerkleRoot(entries); got != want {
		t.Fatalf("single-leaf root = %q, want H(0x00||digest) = %q", got, want)
	}
}

func TestMerkleRootMatchesRFC6962Recurrence(t *testing.T) {
	// Independent known-answer check: recompute the root from the RFC 6962 tree
	// recurrence BY HAND (leaf = H(0x00||digest), node = H(0x01||l||r), split at the
	// largest power of two < n) for n=2..5 and confirm MerkleRoot agrees. This pins
	// the node hashing + unbalanced split against the spec, not just self-consistency.
	leafHash := func(e HarnessLedgerEntry) [sha256.Size]byte {
		data, err := hex.DecodeString(e.Hash)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		h := sha256.New()
		h.Write([]byte{0x00})
		h.Write(data)
		var out [sha256.Size]byte
		copy(out[:], h.Sum(nil))
		return out
	}
	node := func(l, r [sha256.Size]byte) [sha256.Size]byte {
		h := sha256.New()
		h.Write([]byte{0x01})
		h.Write(l[:])
		h.Write(r[:])
		var out [sha256.Size]byte
		copy(out[:], h.Sum(nil))
		return out
	}
	// Reference recurrence implemented directly over leaf hashes.
	var mth func(ls [][sha256.Size]byte) [sha256.Size]byte
	mth = func(ls [][sha256.Size]byte) [sha256.Size]byte {
		switch len(ls) {
		case 1:
			return ls[0]
		default:
			k := 1
			for k<<1 < len(ls) {
				k <<= 1
			}
			return node(mth(ls[:k]), mth(ls[k:]))
		}
	}
	for n := 2; n <= 5; n++ {
		entries := makeEntries(t, n)
		ls := make([][sha256.Size]byte, n)
		for i, e := range entries {
			ls[i] = leafHash(e)
		}
		want := hex.EncodeToString(func() []byte { s := mth(ls); return s[:] }())
		if got := MerkleRoot(entries); got != want {
			t.Fatalf("n=%d: MerkleRoot=%q, hand-computed RFC 6962 MTH=%q", n, got, want)
		}
	}
}

func TestMerkleRootChangesWhenAnyLeafChanges(t *testing.T) {
	entries := makeEntries(t, 5)
	base := MerkleRoot(entries)
	for i := range entries {
		mutated := append([]HarnessLedgerEntry(nil), entries...)
		// Flip the leaf's committed digest (simulating a tampered entry whose Hash
		// was recomputed). The root MUST change for every position.
		mutated[i].Hash = flipLastHexNibble(mutated[i].Hash)
		if MerkleRoot(mutated) == base {
			t.Fatalf("root unchanged after mutating leaf %d — Merkle root is not binding", i)
		}
	}
}

// --- InclusionProof + VerifyInclusion: every leaf, all tree sizes ---

func TestInclusionProofVerifiesForEveryLeafAllSizes(t *testing.T) {
	// Property test: for tree sizes 1..33 (covers powers of two, off-by-one around
	// them, and unbalanced splits), every leaf's inclusion proof must verify against
	// the real root.
	for n := 1; n <= 33; n++ {
		entries := makeEntries(t, n)
		root := MerkleRoot(entries)
		for i := 0; i < n; i++ {
			proof, err := InclusionProof(entries, i)
			if err != nil {
				t.Fatalf("n=%d i=%d: InclusionProof error: %v", n, i, err)
			}
			if proof.TreeSize != n || proof.LeafIndex != i {
				t.Fatalf("n=%d i=%d: proof header mismatch %+v", n, i, proof)
			}
			if !VerifyInclusion(entries[i], proof, root) {
				t.Fatalf("n=%d i=%d: inclusion proof failed to verify (path len %d)", n, i, len(proof.AuditPath))
			}
		}
	}
}

func TestInclusionProofEmptyAndOutOfRange(t *testing.T) {
	if _, err := InclusionProof(nil, 0); err != ErrMerkleEmptyLog {
		t.Fatalf("empty-log InclusionProof err = %v, want ErrMerkleEmptyLog", err)
	}
	entries := makeEntries(t, 4)
	for _, bad := range []int{-1, 4, 99} {
		if _, err := InclusionProof(entries, bad); err == nil {
			t.Fatalf("InclusionProof(i=%d) on size-4 log: expected out-of-range error", bad)
		}
	}
}

func TestVerifyInclusionFailsForTamperedLeaf(t *testing.T) {
	// MANDATORY guard: a tampered leaf must fail inclusion. Build a valid proof,
	// then verify it against a MUTATED entry — the recomputed leaf hash differs so
	// the folded root cannot match.
	entries := makeEntries(t, 6)
	root := MerkleRoot(entries)
	for i := 0; i < len(entries); i++ {
		proof, err := InclusionProof(entries, i)
		if err != nil {
			t.Fatalf("i=%d: %v", i, err)
		}
		tampered := entries[i]
		tampered.Summary = "FORGED summary"              // change the entry content...
		tampered.Hash = flipLastHexNibble(tampered.Hash) // ...and its committed digest
		if VerifyInclusion(tampered, proof, root) {
			t.Fatalf("i=%d: tampered leaf VERIFIED against original root — inclusion is not binding", i)
		}
	}
}

func TestVerifyInclusionFailsForForgedPathOrWrongRoot(t *testing.T) {
	entries := makeEntries(t, 8)
	root := MerkleRoot(entries)
	proof, err := InclusionProof(entries, 3)
	if err != nil {
		t.Fatalf("InclusionProof: %v", err)
	}

	// Forged sibling in the audit path -> cannot reproduce the root.
	forged := proof
	forged.AuditPath = append([]string(nil), proof.AuditPath...)
	forged.AuditPath[0] = flipLastHexNibble(forged.AuditPath[0])
	if VerifyInclusion(entries[3], forged, root) {
		t.Fatalf("forged audit path verified — not binding")
	}

	// Correct proof but wrong (tampered) root must fail.
	if VerifyInclusion(entries[3], proof, flipLastHexNibble(root)) {
		t.Fatalf("inclusion verified against a wrong root")
	}

	// A proof whose path length is wrong for the tree size must be rejected
	// structurally (before hashing).
	tooLong := proof
	tooLong.AuditPath = append(append([]string(nil), proof.AuditPath...), proof.AuditPath[0])
	if VerifyInclusion(entries[3], tooLong, root) {
		t.Fatalf("proof with wrong path length verified")
	}

	// Out-of-range / empty-tree proof headers must be rejected.
	if VerifyInclusion(entries[3], MerkleInclusionProof{TreeSize: 0, LeafIndex: 0}, root) {
		t.Fatalf("empty-tree proof header verified")
	}
}

func TestVerifyInclusionWrongLeafIndexFails(t *testing.T) {
	// A proof built for leaf i must not verify a DIFFERENT leaf entry: presenting
	// entry j (j!=i) under proof-for-i should fail (different leaf hash folded).
	entries := makeEntries(t, 9)
	root := MerkleRoot(entries)
	proof, err := InclusionProof(entries, 4)
	if err != nil {
		t.Fatalf("InclusionProof: %v", err)
	}
	// entries are content-identical except their chained Hash, so each leaf hash is
	// distinct; folding entry 5 under proof-for-4 must not yield the root.
	if VerifyInclusion(entries[5], proof, root) {
		t.Fatalf("proof for index 4 verified a different leaf (index 5)")
	}
}

// --- ConsistencyProof + VerifyConsistency (stretch) ---

func TestConsistencyProofTrivialCases(t *testing.T) {
	entries := makeEntries(t, 5)
	newRoot := MerkleRoot(entries)

	// oldSize == 0: empty proof; empty tree is a prefix of any tree.
	p0, err := ConsistencyProof(0, entries)
	if err != nil {
		t.Fatalf("ConsistencyProof(0): %v", err)
	}
	if len(p0.Path) != 0 {
		t.Fatalf("oldSize=0 should give an empty proof, got %d nodes", len(p0.Path))
	}
	if !VerifyConsistency(p0, EmptyMerkleRoot, newRoot) {
		t.Fatalf("empty-tree-prefix consistency failed to verify")
	}

	// oldSize == NewSize: a tree is consistent with itself; empty proof.
	pSame, err := ConsistencyProof(5, entries)
	if err != nil {
		t.Fatalf("ConsistencyProof(5): %v", err)
	}
	if len(pSame.Path) != 0 {
		t.Fatalf("oldSize==NewSize should give empty proof, got %d", len(pSame.Path))
	}
	if !VerifyConsistency(pSame, newRoot, newRoot) {
		t.Fatalf("self-consistency failed to verify")
	}
}

func TestConsistencyProofOutOfRange(t *testing.T) {
	entries := makeEntries(t, 3)
	for _, bad := range []int{-1, 4, 10} {
		if _, err := ConsistencyProof(bad, entries); err == nil {
			t.Fatalf("ConsistencyProof(oldSize=%d) on size-3 log: expected range error", bad)
		}
	}
}

func TestConsistencyProofVerifiesHonestAppendAllSizes(t *testing.T) {
	// FALSE-POSITIVE guard + core consistency property: for every oldSize m and
	// every newSize n >= m, the proof relating root(entries[:m]) to root(entries[:n])
	// MUST verify — a legitimate append is never flagged inconsistent.
	const maxN = 17
	full := makeEntries(t, maxN)
	for n := 1; n <= maxN; n++ {
		newEntries := full[:n]
		newRoot := MerkleRoot(newEntries)
		for m := 0; m <= n; m++ {
			oldRoot := MerkleRoot(full[:m])
			proof, err := ConsistencyProof(m, newEntries)
			if err != nil {
				t.Fatalf("m=%d n=%d: ConsistencyProof: %v", m, n, err)
			}
			if !VerifyConsistency(proof, oldRoot, newRoot) {
				t.Fatalf("m=%d n=%d: honest-append consistency failed to verify (path len %d)", m, n, len(proof.Path))
			}
		}
	}
}

func TestVerifyConsistencyFailsForRewrittenHistory(t *testing.T) {
	// MANDATORY guard: a REWRITTEN prefix must fail. Build a proof from m->n, then
	// tamper an entry at index < m so the real old root differs. The proof (built
	// over the honest tree) must NOT verify the tampered old root, AND a proof built
	// over a tampered new tree must not verify against the honest old root.
	const m, n = 3, 8
	full := makeEntries(t, n)
	honestOldRoot := MerkleRoot(full[:m])
	honestNewRoot := MerkleRoot(full[:n])
	proof, err := ConsistencyProof(m, full[:n])
	if err != nil {
		t.Fatalf("ConsistencyProof: %v", err)
	}
	if !VerifyConsistency(proof, honestOldRoot, honestNewRoot) {
		t.Fatalf("sanity: honest proof should verify")
	}

	// (a) Same proof, but a FORGED old root (claiming a different prefix) must fail.
	if VerifyConsistency(proof, flipLastHexNibble(honestOldRoot), honestNewRoot) {
		t.Fatalf("consistency verified against a forged old root")
	}
	// (b) Forged new root must fail.
	if VerifyConsistency(proof, honestOldRoot, flipLastHexNibble(honestNewRoot)) {
		t.Fatalf("consistency verified against a forged new root")
	}

	// (c) Actually REWRITE a prefix entry, recompute that tampered tree's roots and
	// proof, and confirm the tampered old root is NOT consistent with the HONEST new
	// root (history was rewritten -> the prefix no longer matches).
	tampered := append([]HarnessLedgerEntry(nil), full...)
	tampered[1].Hash = flipLastHexNibble(tampered[1].Hash) // rewrite an entry < m
	tamperedOldRoot := MerkleRoot(tampered[:m])
	if tamperedOldRoot == honestOldRoot {
		t.Fatalf("test setup: rewriting a prefix entry did not change the old root")
	}
	// The honest proof + honest new root must reject the rewritten old root.
	if VerifyConsistency(proof, tamperedOldRoot, honestNewRoot) {
		t.Fatalf("rewritten-prefix old root verified as consistent with honest new root")
	}
}

func TestVerifyConsistencyRejectsMalformedProof(t *testing.T) {
	entries := makeEntries(t, 8)
	oldRoot := MerkleRoot(entries[:3])
	newRoot := MerkleRoot(entries)
	good, err := ConsistencyProof(3, entries)
	if err != nil {
		t.Fatalf("ConsistencyProof: %v", err)
	}

	// Bad size ordering.
	if VerifyConsistency(MerkleConsistencyProof{OldSize: 5, NewSize: 3}, oldRoot, newRoot) {
		t.Fatalf("verified a proof with OldSize > NewSize")
	}
	// Non-empty path on a oldSize==0 proof is malformed.
	if VerifyConsistency(MerkleConsistencyProof{OldSize: 0, NewSize: 8, Path: good.Path}, EmptyMerkleRoot, newRoot) {
		t.Fatalf("verified a malformed oldSize=0 proof carrying a path")
	}
	// Malformed hex node in the path.
	bad := good
	bad.Path = append([]string(nil), good.Path...)
	bad.Path[0] = "zznothex"
	if VerifyConsistency(bad, oldRoot, newRoot) {
		t.Fatalf("verified a proof with a malformed hex node")
	}
}

// flipLastHexNibble returns the hex string with its final nibble flipped, producing
// a different but still well-formed equal-length hex digest. Used to forge/tamper a
// digest deterministically without changing its length.
func flipLastHexNibble(h string) string {
	if h == "" {
		return "0"
	}
	last := h[len(h)-1]
	var repl byte
	switch {
	case last == '0':
		repl = '1'
	case last >= '1' && last <= '9':
		repl = last - 1
	case last == 'a':
		repl = 'b'
	default: // b..f
		repl = last - 1
	}
	return h[:len(h)-1] + string(repl)
}
