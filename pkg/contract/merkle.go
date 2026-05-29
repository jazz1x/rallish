package contract

// merkle.go — G4+ Merkle verifiability layer: a CT-style (RFC 9162 / RFC 6962)
// Merkle log built ON TOP of the existing append-only ledger entries.
//
// HONEST BOUNDARY (north-star §G4, two orthogonal layers):
//   - The linear prev_hash chain (ChainHash / VerifyChain) is step 1 — it is the
//     ON-WRITE tamper-evidence honoured by the single writer, tamper-evident to a
//     sequential reader who walks every link.
//   - THIS layer is the step-2 verifiability evolution: a PURE READER over an
//     already-loaded []HarnessLedgerEntry that gives O(log n) audit-path proofs —
//     an INCLUSION proof (entry i is in the log under root R) and a CONSISTENCY
//     proof (the log at size m is an append-only PREFIX of the log at size n, i.e.
//     no history was rewritten). Neither replaces the linear chain; both coexist.
//
// It is NOT a trusted-timestamp, witness, gossip, or split-view-detection service:
// those guarantees need a cooperating external auditor (a witness quorum / gossip
// protocol) which is out of scope and stated as deferred. This layer makes proofs
// derivable and verifiable; distributing roots to an external monitor is separate.
//
// LEAF REUSE: the Merkle leaf DATA is each entry's existing Hash field — the exact
// hex SHA-256 that ChainHash already produced for the linear chain. We do NOT
// invent a parallel per-entry hash scheme; the canonical per-entry digest is reused
// verbatim. Per RFC 6962 we then domain-separate with a leaf prefix when hashing
// the tree, so the leaf hash and an interior node hash can never collide
// (second-preimage resistance).
//
// Tree hashing (RFC 6962 §2.1, carried forward by RFC 9162 §2.1):
//
//	MTH({})            = SHA-256()                       // empty tree
//	MTH({d0})          = SHA-256(0x00 || d0)             // single leaf
//	MTH(D[n]), n>1     = SHA-256(0x01 || MTH(D[0:k]) || MTH(D[k:n]))
//	                     where k = largest power of two strictly less than n
//
// PURE: no I/O, no new external deps (crypto/sha256, encoding/hex are already used
// by ChainHash). import-guard stays green — no graph-DB, no graph engine; this is a
// handful of O(n)/O(log n) hashing predicates, not subgraph isomorphism.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// RFC 6962 §2.1 domain-separation prefixes: a leaf is hashed with 0x00 and an
// interior node with 0x01 so a leaf hash can never equal a node hash (prevents a
// second-preimage attack that passes an interior node off as a leaf).
const (
	merkleLeafPrefix byte = 0x00
	merkleNodePrefix byte = 0x01
)

// EmptyMerkleRoot is the defined Merkle Tree Hash of an EMPTY log: the SHA-256 of
// the empty string, per RFC 6962 §2.1 (MTH({}) = SHA-256()). Exposed as a named
// constant so an empty-log root is an explicit, testable contract rather than an
// accidental value.
const EmptyMerkleRoot = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// Merkle proof errors.
var (
	// ErrMerkleIndexOutOfRange is returned by InclusionProof when the requested
	// leaf index is negative or >= the number of entries.
	ErrMerkleIndexOutOfRange = errors.New("contract: merkle leaf index out of range")
	// ErrMerkleEmptyLog is returned by InclusionProof when the log has no entries:
	// there is no leaf to prove inclusion of.
	ErrMerkleEmptyLog = errors.New("contract: merkle inclusion proof requested on empty log")
	// ErrMerkleConsistencyRange is returned by ConsistencyProof when oldSize is
	// negative or larger than the current number of entries (a consistency proof
	// only relates an OLD size to a CURRENT size that is at least as large).
	ErrMerkleConsistencyRange = errors.New("contract: merkle consistency oldSize out of range")
)

// merkleLeafHash hashes one leaf's DATA with the RFC 6962 leaf prefix. The data is
// the entry's existing Hash (the ChainHash output) decoded from hex back to its raw
// 32 bytes, so the Merkle tree commits to the SAME per-entry digest the linear
// chain uses. If the stored Hash is not valid hex (e.g. an un-chained or corrupted
// entry) we fall back to hashing the raw string bytes — the leaf is still
// deterministic and any later mutation still changes the root; the prefix keeps it
// distinct from a node.
func merkleLeafHash(entry HarnessLedgerEntry) [sha256.Size]byte {
	leafData, err := hex.DecodeString(entry.Hash)
	if err != nil {
		leafData = []byte(entry.Hash)
	}
	h := sha256.New()
	h.Write([]byte{merkleLeafPrefix})
	h.Write(leafData)
	var out [sha256.Size]byte
	copy(out[:], h.Sum(nil))
	return out
}

// merkleNodeHash hashes the concatenation of two child node hashes with the RFC
// 6962 interior-node prefix.
func merkleNodeHash(left, right [sha256.Size]byte) [sha256.Size]byte {
	h := sha256.New()
	h.Write([]byte{merkleNodePrefix})
	h.Write(left[:])
	h.Write(right[:])
	var out [sha256.Size]byte
	copy(out[:], h.Sum(nil))
	return out
}

// largestPowerOfTwoLessThan returns the largest power of two strictly less than n,
// for n > 1. This is the split point k in RFC 6962's MTH recurrence: the left
// subtree is a PERFECT (complete) binary tree of k leaves and the right subtree
// holds the remaining n-k leaves. Requires n >= 2.
func largestPowerOfTwoLessThan(n int) int {
	k := 1
	for k<<1 < n {
		k <<= 1
	}
	return k
}

// merkleTreeHash computes the RFC 6962 Merkle Tree Hash (MTH) over leaves[lo:hi]
// recursively. The caller's slice of precomputed leaf hashes is read-only.
func merkleTreeHash(leaves [][sha256.Size]byte) [sha256.Size]byte {
	n := len(leaves)
	switch n {
	case 0:
		// MTH({}) = SHA-256() — the hash of the empty input.
		var out [sha256.Size]byte
		copy(out[:], sha256.New().Sum(nil))
		return out
	case 1:
		// The leaf hashes are already H(0x00 || data); a single-leaf tree's MTH IS
		// that leaf hash.
		return leaves[0]
	default:
		k := largestPowerOfTwoLessThan(n)
		left := merkleTreeHash(leaves[:k])
		right := merkleTreeHash(leaves[k:])
		return merkleNodeHash(left, right)
	}
}

// computeLeafHashes precomputes the per-leaf hashes for all entries once, so the
// root and any proof share identical leaf inputs.
func computeLeafHashes(entries []HarnessLedgerEntry) [][sha256.Size]byte {
	leaves := make([][sha256.Size]byte, len(entries))
	for i, e := range entries {
		leaves[i] = merkleLeafHash(e)
	}
	return leaves
}

// MerkleRoot builds the RFC 6962 / RFC 9162 Merkle Tree Hash over the ledger
// entries and returns it hex-encoded. The leaves are the per-entry ChainHash
// digests (reused, not recomputed). It is deterministic for a given entry slice,
// and an EMPTY log returns the defined EmptyMerkleRoot.
//
// This is the orthogonal verifiability root; the linear prev_hash chain is
// unaffected (both layers coexist — see the package doc on this file).
func MerkleRoot(entries []HarnessLedgerEntry) (root string) {
	leaves := computeLeafHashes(entries)
	sum := merkleTreeHash(leaves)
	return hex.EncodeToString(sum[:])
}

// MerkleInclusionProof is an RFC 9162 §2.1.3.1 inclusion (audit) proof: the ordered
// list of sibling node hashes on the path from leaf LeafIndex up to the root of a
// tree of TreeSize leaves. With the leaf data and the root, a verifier recomputes
// the root and confirms the leaf is committed at that index.
type MerkleInclusionProof struct {
	// LeafIndex is the 0-based index of the proven leaf in the log.
	LeafIndex int
	// TreeSize is the number of leaves (entries) the proof was built against; the
	// proof and root only relate at this size.
	TreeSize int
	// AuditPath is the bottom-up list of sibling hashes (hex-encoded). Its length
	// is ceil(log2(TreeSize)) for the relevant subtree path.
	AuditPath []string
}

// InclusionProof produces an RFC 9162 inclusion proof that entry i is the i-th leaf
// of the Merkle tree over entries (a tree of size len(entries)). It returns
// ErrMerkleEmptyLog for an empty log and ErrMerkleIndexOutOfRange for an i outside
// [0, len(entries)).
//
// PURE reader: it only reads entries; it computes the audit path via the RFC 6962
// PATH(m, D) recurrence.
func InclusionProof(entries []HarnessLedgerEntry, i int) (MerkleInclusionProof, error) {
	n := len(entries)
	if n == 0 {
		return MerkleInclusionProof{}, ErrMerkleEmptyLog
	}
	if i < 0 || i >= n {
		return MerkleInclusionProof{}, fmt.Errorf("%w: index %d not in [0,%d)", ErrMerkleIndexOutOfRange, i, n)
	}
	leaves := computeLeafHashes(entries)
	path := inclusionPath(i, leaves)
	hexPath := make([]string, len(path))
	for j, p := range path {
		hexPath[j] = hex.EncodeToString(p[:])
	}
	return MerkleInclusionProof{LeafIndex: i, TreeSize: n, AuditPath: hexPath}, nil
}

// inclusionPath implements RFC 6962 §2.1.1 PATH(m, D[n]) — the audit path for the
// m-th leaf of the tree over leaves D[n]. The returned hashes are ordered
// bottom-up (closest sibling first).
//
//	PATH(0, {d0}) = {}                                  // single leaf: empty path
//	PATH(m, D[n]), n>1, m<k  = PATH(m, D[0:k])   : MTH(D[k:n])
//	PATH(m, D[n]), n>1, m>=k = PATH(m-k, D[k:n]) : MTH(D[0:k])
//	  where k = largest power of two strictly less than n.
func inclusionPath(m int, leaves [][sha256.Size]byte) [][sha256.Size]byte {
	n := len(leaves)
	if n == 1 {
		return nil
	}
	k := largestPowerOfTwoLessThan(n)
	if m < k {
		// Leaf is in the left subtree; sibling is the right subtree's hash.
		sub := inclusionPath(m, leaves[:k])
		return append(sub, merkleTreeHash(leaves[k:]))
	}
	// Leaf is in the right subtree; sibling is the left subtree's hash.
	sub := inclusionPath(m-k, leaves[k:])
	return append(sub, merkleTreeHash(leaves[:k]))
}

// VerifyInclusion checks an RFC 9162 inclusion proof: starting from leafEntry's leaf
// hash (recomputed locally — the verifier does NOT trust a supplied leaf hash) it
// folds in each audit-path sibling in order, deciding left/right by the bit pattern
// of the leaf index within the tree, and confirms the reconstructed root equals the
// supplied root.
//
// A TAMPERED leaf fails: a mutated entry yields a different leaf hash, so the folded
// root cannot match. A FORGED audit path (wrong siblings) likewise cannot reproduce
// the root. Returns true only when the proof is internally consistent AND
// reproduces root.
func VerifyInclusion(leafEntry HarnessLedgerEntry, proof MerkleInclusionProof, root string) bool {
	// Defensive structural checks first; an out-of-range index or a path length
	// that disagrees with the tree size is not a valid proof.
	if proof.TreeSize <= 0 || proof.LeafIndex < 0 || proof.LeafIndex >= proof.TreeSize {
		return false
	}
	if len(proof.AuditPath) != inclusionPathLen(proof.LeafIndex, proof.TreeSize) {
		return false
	}
	// Recompute the leaf hash from the entry itself — never trust a caller-supplied
	// leaf digest.
	acc := merkleLeafHash(leafEntry)
	// Fold the BOTTOM-UP audit path with the RFC 9162 §2.1.3.2 algorithm. fn is the
	// leaf's index and sn the index of the LAST leaf (size-1), both within the
	// current sub-tree; their low bits drive the left/right decision and are shifted
	// down one level per consumed sibling. A sibling is a LEFT sibling (node = H(sib
	// || acc)) when the current node is a right child (fn is odd) or sits on the
	// tree's right edge (fn == sn); otherwise it is a RIGHT sibling.
	fn, sn := proof.LeafIndex, proof.TreeSize-1
	for _, sibHex := range proof.AuditPath {
		sib, err := hex.DecodeString(sibHex)
		if err != nil || len(sib) != sha256.Size {
			return false
		}
		var sibArr [sha256.Size]byte
		copy(sibArr[:], sib)
		if fn%2 == 1 || fn == sn {
			acc = merkleNodeHash(sibArr, acc)
			// Skip the run of "last node on this edge" levels: shift until fn is odd
			// or the sub-tree collapses.
			for fn%2 == 0 && fn != 0 {
				fn >>= 1
				sn >>= 1
			}
		} else {
			acc = merkleNodeHash(acc, sibArr)
		}
		fn >>= 1
		sn >>= 1
	}
	return hex.EncodeToString(acc[:]) == root
}

// inclusionPathLen returns the expected audit-path length for leaf m in a tree of
// size n (n >= 1), mirroring inclusionPath's recursion so VerifyInclusion can
// reject a proof whose path length is wrong before doing any hashing.
func inclusionPathLen(m, n int) int {
	if n <= 1 {
		return 0
	}
	k := largestPowerOfTwoLessThan(n)
	if m < k {
		return inclusionPathLen(m, k) + 1
	}
	return inclusionPathLen(m-k, n-k) + 1
}

// MerkleConsistencyProof is an RFC 9162 §2.1.4 consistency proof: the minimal set
// of node hashes proving the Merkle tree at OldSize leaves is an append-only PREFIX
// of the tree at NewSize leaves (no entry at index < OldSize was altered or
// reordered). With the old root and the new root, a verifier confirms both.
type MerkleConsistencyProof struct {
	// OldSize is the smaller (earlier) tree size the proof relates from.
	OldSize int
	// NewSize is the larger (current) tree size the proof relates to.
	NewSize int
	// Path is the ordered list of node hashes (hex-encoded) per RFC 6962 PROOF.
	Path []string
}

// ConsistencyProof produces an RFC 9162 consistency proof between the tree of the
// first oldSize entries and the full tree of len(entries) entries. It proves the
// older log is an append-only prefix of the newer one.
//
// Edge cases (RFC 6962 §2.1.2): oldSize == 0 yields an empty proof (the empty tree
// is a prefix of any tree); oldSize == NewSize yields an empty proof (a tree is
// trivially consistent with itself). Returns ErrMerkleConsistencyRange if oldSize
// is negative or larger than len(entries).
//
// PURE reader. NOTE on the threat model: this proves the bytes are an append-only
// prefix; it does NOT by itself defend against a SPLIT VIEW (a log presenting
// different roots to different readers) — detecting that needs an external
// witness/gossip auditor, stated as deferred in this file's package doc.
func ConsistencyProof(oldSize int, entries []HarnessLedgerEntry) (MerkleConsistencyProof, error) {
	n := len(entries)
	if oldSize < 0 || oldSize > n {
		return MerkleConsistencyProof{}, fmt.Errorf("%w: oldSize %d not in [0,%d]", ErrMerkleConsistencyRange, oldSize, n)
	}
	leaves := computeLeafHashes(entries)
	var path [][sha256.Size]byte
	if oldSize != 0 && oldSize != n {
		// RFC 6962 SUBPROOF with the top-level node b=true (the old tree is, at this
		// level, a complete subtree on the left — i.e. m starts "on the path").
		path = subProof(oldSize, leaves, true)
	}
	hexPath := make([]string, len(path))
	for j, p := range path {
		hexPath[j] = hex.EncodeToString(p[:])
	}
	return MerkleConsistencyProof{OldSize: oldSize, NewSize: n, Path: hexPath}, nil
}

// subProof implements RFC 6962 §2.1.2 SUBPROOF(m, D[n], b). It returns the node
// hashes of the consistency proof relating the tree over D[0:m] to the tree over
// D[0:n] (m <= n). The boolean b tracks whether the subtree rooted here is itself a
// complete left-edge subtree of the OLD tree (b=true means "m exactly covers this
// subtree", so its own root hash is omitted unless we are at a boundary).
//
//	SUBPROOF(m, D[m], true)  = {}                          // m == n, on the path
//	SUBPROOF(m, D[m], false) = { MTH(D[m]) }               // m == n, off the path
//	SUBPROOF(m, D[n], b), m<n, m<=k =
//	    SUBPROOF(m, D[0:k], b) : MTH(D[k:n])
//	SUBPROOF(m, D[n], b), m<n, m>k =
//	    SUBPROOF(m-k, D[k:n], false) : MTH(D[0:k])
//	  where k = largest power of two strictly less than n.
func subProof(m int, leaves [][sha256.Size]byte, b bool) [][sha256.Size]byte {
	n := len(leaves)
	if m == n {
		if b {
			return nil
		}
		return [][sha256.Size]byte{merkleTreeHash(leaves)}
	}
	k := largestPowerOfTwoLessThan(n)
	if m <= k {
		// The old boundary falls within (or at the edge of) the left subtree; recurse
		// left, then append the right subtree's root.
		sub := subProof(m, leaves[:k], b)
		return append(sub, merkleTreeHash(leaves[k:]))
	}
	// The old boundary is in the right subtree; recurse right (now off the complete
	// left edge, so b=false), then append the left subtree's root.
	sub := subProof(m-k, leaves[k:], false)
	return append(sub, merkleTreeHash(leaves[:k]))
}

// VerifyConsistency checks an RFC 9162 consistency proof: given the proof, the old
// root (tree of proof.OldSize leaves) and the new root (tree of proof.NewSize
// leaves), it confirms the old tree is an append-only prefix of the new tree.
//
// It implements RFC 6962 §2.1.2's verification recurrence. A REWRITTEN history (an
// entry at index < OldSize changed) makes the reconstructed old root differ from
// the supplied oldRoot, so verification fails — that is the false-NEGATIVE-free
// guarantee. A legitimate append (same prefix, new entries appended) verifies — the
// false-POSITIVE guard. Returns true only when BOTH the recomputed old root matches
// oldRoot AND the recomputed new root matches newRoot.
func VerifyConsistency(proof MerkleConsistencyProof, oldRoot, newRoot string) bool {
	first, second := proof.OldSize, proof.NewSize
	if first < 0 || second < 0 || first > second {
		return false
	}
	// RFC 6962 §2.1.2 trivial cases: an empty old tree, or equal sizes, carry an
	// empty proof and just require the old root to be consistent by definition.
	if first == 0 {
		// The empty tree (EmptyMerkleRoot) is a prefix of everything; we cannot
		// reconstruct anything from an empty proof, so we only assert the new root is
		// well-formed hex of the right length. The old root must be the empty root.
		if len(proof.Path) != 0 {
			return false
		}
		return oldRoot == EmptyMerkleRoot && isHexSize(newRoot)
	}
	if first == second {
		// A tree is consistent with itself; the proof is empty and both roots equal.
		return len(proof.Path) == 0 && oldRoot == newRoot && isHexSize(oldRoot)
	}

	path, ok := decodeNodes(proof.Path)
	if !ok || len(path) == 0 {
		return false
	}

	// RFC 9162 §2.1.4.2 verification. fn/sn are the indices of the LAST leaf of the
	// old and new trees respectively; we first shift them right while fn is odd so
	// the loop starts at the boundary level. After that shift, fn == 0 iff `first`
	// was an exact power of two — then the old tree is a complete subtree whose root
	// is NOT carried in the proof and is seeded directly; otherwise the first proof
	// node is that seed.
	fn, sn := first-1, second-1
	for fn%2 == 1 {
		fn >>= 1
		sn >>= 1
	}
	var idx int
	var fr, sr [sha256.Size]byte // running hashes toward the old root and new root
	if fn == 0 {
		// `first` is a power of two: seed both from the (claimed) old root.
		fr = decodeRoot(oldRoot)
		sr = fr
		idx = 0
	} else {
		fr = path[0]
		sr = path[0]
		idx = 1
	}

	for ; idx < len(path); idx++ {
		node := path[idx]
		if sn == 0 {
			// The new tree's structure is exhausted but proof nodes remain: malformed.
			return false
		}
		if fn%2 == 1 || fn == sn {
			// Current node is a right child (fn odd) or sits on the right edge
			// (fn == sn): the proof node is a LEFT sibling for BOTH running hashes.
			fr = merkleNodeHash(node, fr)
			sr = merkleNodeHash(node, sr)
			// Skip the run of complete left subtrees on the old boundary.
			for fn%2 == 0 && fn != 0 {
				fn >>= 1
				sn >>= 1
			}
		} else {
			// The proof node is a RIGHT sibling of the new tree only.
			sr = merkleNodeHash(sr, node)
		}
		fn >>= 1
		sn >>= 1
	}

	// A well-formed proof consumes exactly the new tree's right edge (sn == 0) and
	// reconstructs BOTH roots.
	return sn == 0 && hex.EncodeToString(fr[:]) == oldRoot && hex.EncodeToString(sr[:]) == newRoot
}

// isHexSize reports whether s decodes to exactly a SHA-256 digest's worth of bytes.
func isHexSize(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size
}

// decodeRoot decodes a hex root into a fixed array (zero array on malformed input;
// callers compare the final hex string, so a malformed seed simply fails to match).
func decodeRoot(s string) [sha256.Size]byte {
	var out [sha256.Size]byte
	b, err := hex.DecodeString(s)
	if err == nil && len(b) == sha256.Size {
		copy(out[:], b)
	}
	return out
}

// decodeNodes decodes a hex proof path into fixed arrays; ok=false if any element
// is malformed or the wrong length.
func decodeNodes(hexNodes []string) (nodes [][sha256.Size]byte, ok bool) {
	nodes = make([][sha256.Size]byte, len(hexNodes))
	for i, h := range hexNodes {
		b, err := hex.DecodeString(h)
		if err != nil || len(b) != sha256.Size {
			return nil, false
		}
		copy(nodes[i][:], b)
	}
	return nodes, true
}
