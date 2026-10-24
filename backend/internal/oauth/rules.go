package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/url"
	"regexp"
	"slices"
)

// The rules a registration and an authorization request are held to, adapted
// from Exciter's backend/internal/oauth/rules.go.

// verifierShape is RFC 7636's code_verifier: 43 to 128 unreserved characters.
var verifierShape = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

// challengeShape is an S256 challenge: the verifier's SHA-256, base64url without padding.
var challengeShape = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// VerifyPKCE says whether a verifier is the one an S256 challenge was made from.
func VerifyPKCE(challenge, verifier string) bool {
	if !verifierShape.MatchString(verifier) || !challengeShape.MatchString(challenge) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

// loopback is how a tool on the person's own machine is reached, at whichever
// port it listens on (RFC 8252).
func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validRedirect is a return address an app may register: https, or http on a
// loopback address, without a fragment or credentials.
func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Fragment != "" || u.User != nil || u.Host == "" || u.Opaque != "" {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && loopback(u.Hostname()))
}

// RedirectMatches says whether a return address is one the app registered,
// any port of a loopback one matching, as a local tool picks it anew each run.
func RedirectMatches(registered []string, asked string) bool {
	if slices.Contains(registered, asked) {
		return true
	}
	a, err := url.Parse(asked)
	if err != nil || a.Scheme != "http" || !loopback(a.Hostname()) || a.Fragment != "" || a.User != nil {
		return false
	}
	for _, each := range registered {
		r, err := url.Parse(each)
		if err == nil && r.Scheme == a.Scheme && r.Hostname() == a.Hostname() && r.Path == a.Path && r.RawQuery == a.RawQuery {
			return true
		}
	}
	return false
}

// redirectHost is the host a person is told the browser goes back to.
func redirectHost(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Host
	}
	return raw
}
