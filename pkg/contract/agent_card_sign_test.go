package contract

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"testing"
)

// sampleCard returns a representative unsigned AgentCard for signing tests.
func sampleCard() AgentCard {
	return AgentCard{
		ProtocolVersion:  ProtocolVersion,
		Name:             "rallish",
		Description:      "A local broker for multi-agent turn-taking",
		Version:          "0.3.0",
		URL:              "http://localhost:8080",
		DocumentationURL: "https://github.com/jazz1x/rallish",
		Capabilities:     AgentCapability{Streaming: true},
		Skills: []AgentSkill{
			{ID: "pair-review", Name: "Pair Review", Tags: []string{"review"}},
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
	}
}

func mustKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

// TestSignVerifyRoundTrip is the happy path AND the mandatory false-positive
// guard: a validly signed card MUST verify (rallish is fail-open; a legitimate
// card must never be rejected).
func TestSignVerifyRoundTrip(t *testing.T) {
	_, priv := mustKey(t)
	signed, err := SignAgentCard(sampleCard(), priv)
	if err != nil {
		t.Fatalf("SignAgentCard: %v", err)
	}
	if signed.Signature == nil {
		t.Fatal("signed card has nil Signature")
	}
	if signed.Signature.Algorithm != SignatureAlgorithm {
		t.Errorf("Algorithm = %q, want %q", signed.Signature.Algorithm, SignatureAlgorithm)
	}
	if err := VerifyAgentCard(signed); err != nil {
		t.Errorf("VerifyAgentCard(validly signed) = %v, want nil", err)
	}
}

// TestSignDoesNotMutateInput confirms signing returns a copy and leaves the
// caller's card unsigned (no surprise aliasing).
func TestSignDoesNotMutateInput(t *testing.T) {
	_, priv := mustKey(t)
	in := sampleCard()
	if _, err := SignAgentCard(in, priv); err != nil {
		t.Fatalf("SignAgentCard: %v", err)
	}
	if in.Signature != nil {
		t.Error("SignAgentCard mutated the input card's Signature")
	}
}

// TestTamperedCardFailsVerification mutates each kind of field AFTER signing and
// requires verification to fail with ErrSignatureMismatch.
func TestTamperedCardFailsVerification(t *testing.T) {
	_, priv := mustKey(t)
	base, err := SignAgentCard(sampleCard(), priv)
	if err != nil {
		t.Fatalf("SignAgentCard: %v", err)
	}

	mutations := map[string]func(*AgentCard){
		"scalar-name":   func(c *AgentCard) { c.Name = "evil" },
		"scalar-url":    func(c *AgentCard) { c.URL = "http://attacker" },
		"nested-stream": func(c *AgentCard) { c.Capabilities.Streaming = false },
		"slice-skill":   func(c *AgentCard) { c.Skills = append(c.Skills, AgentSkill{ID: "x"}) },
		"slice-modes":   func(c *AgentCard) { c.DefaultInputModes = append(c.DefaultInputModes, "evil/mode") },
		"protocol-ver":  func(c *AgentCard) { c.ProtocolVersion = "9.9" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			tampered := base
			// Deep-copy the slices we mutate so cases don't bleed into each other.
			tampered.Skills = append([]AgentSkill(nil), base.Skills...)
			tampered.DefaultInputModes = append([]string(nil), base.DefaultInputModes...)
			sig := *base.Signature
			tampered.Signature = &sig
			mutate(&tampered)
			if err := VerifyAgentCard(tampered); !errors.Is(err, ErrSignatureMismatch) {
				t.Errorf("VerifyAgentCard(tampered %s) = %v, want ErrSignatureMismatch", name, err)
			}
		})
	}
}

// TestNonCircular proves the Signature field is excluded from its own signed
// input: mutating the Signature bytes (without re-signing) must NOT make the
// content fail to canonicalize, and the canonical bytes are identical whether or
// not a signature is attached — i.e. the signature does not feed its own input.
func TestNonCircular(t *testing.T) {
	_, priv := mustKey(t)
	signed, err := SignAgentCard(sampleCard(), priv)
	if err != nil {
		t.Fatalf("SignAgentCard: %v", err)
	}

	unsignedCanon, err := canonicalAgentCardBytes(sampleCard())
	if err != nil {
		t.Fatalf("canonical unsigned: %v", err)
	}
	signedCanon, err := canonicalAgentCardBytes(signed)
	if err != nil {
		t.Fatalf("canonical signed: %v", err)
	}
	if string(unsignedCanon) != string(signedCanon) {
		t.Fatalf("canonical form changed when a signature was attached:\n unsigned=%s\n signed=%s", unsignedCanon, signedCanon)
	}

	// Swapping the signature for a different valid-but-wrong signature over the
	// SAME content must still verify-fail (mismatch), not change the canonical
	// content. This confirms the signature is purely an output, never an input.
	_, otherPriv := mustKey(t)
	otherSigned, err := SignAgentCard(sampleCard(), otherPriv)
	if err != nil {
		t.Fatalf("SignAgentCard(other): %v", err)
	}
	frankencard := signed
	frankencard.Signature = otherSigned.Signature // wrong key's sig+pub on this card content
	// otherSigned signed identical content with a different key, so it actually
	// verifies (the embedded pub matches). The point: canonical bytes are stable.
	reCanon, err := canonicalAgentCardBytes(frankencard)
	if err != nil {
		t.Fatalf("canonical franken: %v", err)
	}
	if string(reCanon) != string(signedCanon) {
		t.Error("canonical bytes depended on the signature field (circular)")
	}
}

// TestUnsignedCardVerifiesAsUnsigned ensures an unsigned card returns
// ErrUnsignedCard (NOT a panic, NOT ErrSignatureMismatch) so absence-of-signature
// is never mistaken for a forgery.
func TestUnsignedCardVerifiesAsUnsigned(t *testing.T) {
	if err := VerifyAgentCard(sampleCard()); !errors.Is(err, ErrUnsignedCard) {
		t.Errorf("VerifyAgentCard(unsigned) = %v, want ErrUnsignedCard", err)
	}
}

// TestVerifyRejectsBadAlgorithm guards the strict-alg path.
func TestVerifyRejectsBadAlgorithm(t *testing.T) {
	_, priv := mustKey(t)
	signed, err := SignAgentCard(sampleCard(), priv)
	if err != nil {
		t.Fatalf("SignAgentCard: %v", err)
	}
	sig := *signed.Signature
	sig.Algorithm = "HS256"
	signed.Signature = &sig
	if err := VerifyAgentCard(signed); !errors.Is(err, ErrBadSignatureAlgorithm) {
		t.Errorf("VerifyAgentCard(bad alg) = %v, want ErrBadSignatureAlgorithm", err)
	}
}

// TestVerifyRejectsMalformedSignature covers bad base64 and wrong key lengths.
func TestVerifyRejectsMalformedSignature(t *testing.T) {
	_, priv := mustKey(t)
	signed, err := SignAgentCard(sampleCard(), priv)
	if err != nil {
		t.Fatalf("SignAgentCard: %v", err)
	}
	cases := map[string]func(*AgentCardSignature){
		"bad-base64-pubkey": func(s *AgentCardSignature) { s.PublicKey = "!!!not base64!!!" },
		"bad-base64-sig":    func(s *AgentCardSignature) { s.Signature = "!!!not base64!!!" },
		"short-pubkey":      func(s *AgentCardSignature) { s.PublicKey = base64.StdEncoding.EncodeToString([]byte("short")) },
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			sig := *signed.Signature
			corrupt(&sig)
			c := signed
			c.Signature = &sig
			if err := VerifyAgentCard(c); !errors.Is(err, ErrMalformedSignature) {
				t.Errorf("VerifyAgentCard(%s) = %v, want ErrMalformedSignature", name, err)
			}
		})
	}
}

// TestSignRejectsBadPrivateKey guards the signer-side key-length check.
func TestSignRejectsBadPrivateKey(t *testing.T) {
	if _, err := SignAgentCard(sampleCard(), ed25519.PrivateKey("too-short")); !errors.Is(err, ErrMalformedSignature) {
		t.Errorf("SignAgentCard(bad key) error = %v, want ErrMalformedSignature", err)
	}
}

// TestSignatureDeterministic confirms signing the same card with the same key
// twice yields byte-identical signatures (ed25519 is deterministic; the
// canonical form is stable), so the audit/record layer sees a stable artifact.
func TestSignatureDeterministic(t *testing.T) {
	_, priv := mustKey(t)
	a, err := SignAgentCard(sampleCard(), priv)
	if err != nil {
		t.Fatalf("SignAgentCard a: %v", err)
	}
	b, err := SignAgentCard(sampleCard(), priv)
	if err != nil {
		t.Fatalf("SignAgentCard b: %v", err)
	}
	if a.Signature.Signature != b.Signature.Signature {
		t.Errorf("non-deterministic signature:\n a=%s\n b=%s", a.Signature.Signature, b.Signature.Signature)
	}
	if a.Signature.PublicKey != b.Signature.PublicKey {
		t.Error("public key differed across signings with the same key")
	}
}
