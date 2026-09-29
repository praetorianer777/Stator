package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// Session secrets and personal access tokens are never stored, only their
// SHA-256, so a copy of the database opens nothing. A plain hash rather than
// argon2, because the secret is 256 random bits: there is nothing to guess,
// and the lookup runs on every request.

const (
	// tokenBytes is the raw entropy behind a token, before encoding.
	tokenBytes = 32

	// APITokenPrefix marks a personal access token, so a secret scanner, a log
	// redaction and a person can each recognise one on sight.
	APITokenPrefix = "stator_pat_"
)

// GenerateToken returns a new opaque secret and the digest to store for it.
func GenerateToken() (secret string, digest []byte, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	secret = base64.RawURLEncoding.EncodeToString(raw)
	return secret, HashToken(secret), nil
}

// GenerateAPIToken returns a prefixed personal access token and its digest.
func GenerateAPIToken() (secret string, digest []byte, err error) {
	s, _, err := GenerateToken()
	if err != nil {
		return "", nil, err
	}
	secret = APITokenPrefix + s
	return secret, HashToken(secret), nil
}

// HashToken returns the digest stored for a token secret.
func HashToken(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// IsAPIToken reports whether a presented credential claims to be a personal
// access token rather than a session secret.
func IsAPIToken(secret string) bool {
	return strings.HasPrefix(secret, APITokenPrefix)
}

// ParseAPIToken checks that a personal access token has the shape
// GenerateAPIToken gives one, so a malformed one costs no database round trip.
func ParseAPIToken(secret string) error {
	body, ok := strings.CutPrefix(secret, APITokenPrefix)
	if !ok {
		return ErrInvalidToken
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(body)
	if err != nil || len(raw) != tokenBytes {
		return ErrInvalidToken
	}
	return nil
}
