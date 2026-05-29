package contract

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// SignatureAlgorithm is the only signature algorithm rallish produces or accepts
// for the AgentCard today: EdDSA over ed25519 (JOSE "alg" naming). A card whose
// AgentCardSignature.Algorithm differs fails verification rather than being
// silently accepted (strict, not liberal, at the interface — RFC 9413).
const SignatureAlgorithm = "EdDSA"

// Sentinel errors returned by VerifyAgentCard so callers can branch on the cause
// with errors.Is rather than parsing strings. ErrUnsignedCard distinguishes "no
// signature present" (an unsigned card, not a tamper) from an actual bad
// signature, so an unsigned card is never mistaken for a forged one.
var (
	// ErrUnsignedCard is returned when the card carries no signature at all.
	ErrUnsignedCard = errors.New("agent card is unsigned")
	// ErrBadSignatureAlgorithm is returned when the signature's algorithm is not
	// the supported SignatureAlgorithm.
	ErrBadSignatureAlgorithm = errors.New("unsupported agent card signature algorithm")
	// ErrMalformedSignature is returned when the public key or signature bytes are
	// not valid base64 or the public key is not a valid ed25519 key length.
	ErrMalformedSignature = errors.New("malformed agent card signature")
	// ErrSignatureMismatch is returned when the ed25519 signature does not verify
	// against the canonical card content and embedded public key (tampered card or
	// wrong key).
	ErrSignatureMismatch = errors.New("agent card signature does not verify")
)

// canonicalAgentCardBytes returns the deterministic byte serialization of a card
// with its Signature field EXCLUDED, the single SSOT both SignAgentCard and
// VerifyAgentCard sign/verify over so they cannot drift.
//
// Non-circularity: the Signature field is the thing being produced, so it must
// not feed its own input — it is zeroed before marshaling here. This is the same
// trick ChainHash uses to exclude its own Hash field from the hashed content.
//
// Determinism (no JCS dependency): AgentCard is a fixed Go struct containing no
// maps, so encoding/json marshals its fields in declaration order with no
// insignificant whitespace — already canonical for a fixed shape. RFC 8785 JCS
// would only be required if cards were re-serialized from untyped maps, which is
// out of scope here.
func canonicalAgentCardBytes(card AgentCard) ([]byte, error) {
	card.Signature = nil
	b, err := json.Marshal(card)
	if err != nil {
		return nil, fmt.Errorf("canonicalize agent card: %w", err)
	}
	return b, nil
}

// SignAgentCard returns a copy of card carrying an embedded ed25519 signature
// (F16 — verifiable interop). It signs over the canonical form of the card with
// the Signature field excluded, then embeds the algorithm, the signer's public
// key, and the signature so a client can verify authenticity without an
// out-of-band key fetch.
//
// The input card is not mutated; the returned card is the signed copy. The
// caller supplies the ed25519 private key — key generation, storage, rotation,
// and distribution are DEFERRED (see AgentCardSignature's key-management
// boundary). Honest naming: this is a signed card with a caller-supplied key.
func SignAgentCard(card AgentCard, priv ed25519.PrivateKey) (AgentCard, error) {
	if l := len(priv); l != ed25519.PrivateKeySize {
		return AgentCard{}, fmt.Errorf("%w: private key is %d bytes, want %d", ErrMalformedSignature, l, ed25519.PrivateKeySize)
	}
	msg, err := canonicalAgentCardBytes(card)
	if err != nil {
		return AgentCard{}, err
	}
	sig := ed25519.Sign(priv, msg)
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return AgentCard{}, fmt.Errorf("%w: private key did not yield an ed25519 public key", ErrMalformedSignature)
	}
	card.Signature = &AgentCardSignature{
		Algorithm: SignatureAlgorithm,
		PublicKey: base64.StdEncoding.EncodeToString(pub),
		Signature: base64.StdEncoding.EncodeToString(sig),
	}
	return card, nil
}

// VerifyAgentCard verifies an embedded ed25519 signature on a card (F16). It
// re-derives the same canonical form SignAgentCard signed (Signature excluded —
// non-circular) and checks the ed25519 signature against the embedded public
// key.
//
// It returns a typed error so a caller can distinguish causes with errors.Is:
// ErrUnsignedCard (no signature — NOT a tamper), ErrBadSignatureAlgorithm,
// ErrMalformedSignature (bad base64 / wrong key length), or ErrSignatureMismatch
// (a tampered card or wrong key). A nil return means the signature is valid.
//
// Trust boundary: a valid signature proves the card content matches what the
// holder of the embedded key signed; it does NOT establish that the embedded key
// is the *expected* signer — that trust is out-of-band (key distribution
// deferred).
func VerifyAgentCard(card AgentCard) error {
	if card.Signature == nil {
		return ErrUnsignedCard
	}
	if card.Signature.Algorithm != SignatureAlgorithm {
		return fmt.Errorf("%w: %q", ErrBadSignatureAlgorithm, card.Signature.Algorithm)
	}
	pubBytes, err := base64.StdEncoding.DecodeString(card.Signature.PublicKey)
	if err != nil {
		return fmt.Errorf("%w: public key: %v", ErrMalformedSignature, err)
	}
	if l := len(pubBytes); l != ed25519.PublicKeySize {
		return fmt.Errorf("%w: public key is %d bytes, want %d", ErrMalformedSignature, l, ed25519.PublicKeySize)
	}
	sigBytes, err := base64.StdEncoding.DecodeString(card.Signature.Signature)
	if err != nil {
		return fmt.Errorf("%w: signature: %v", ErrMalformedSignature, err)
	}
	msg, err := canonicalAgentCardBytes(card)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(pubBytes), msg, sigBytes) {
		return ErrSignatureMismatch
	}
	return nil
}
