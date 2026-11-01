package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// randomToken is 32 bytes of r in base64url without padding: 43
// characters, a state or a PKCE verifier (RFC 7636 §4.1: 43 to 128).
func randomToken(r io.Reader) (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", fmt.Errorf("oauth: random bytes: %w", err)
	}
	defer clear(b)
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// challengeS256 is the S256 code challenge of verifier (RFC 7636 §4.2).
func challengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
