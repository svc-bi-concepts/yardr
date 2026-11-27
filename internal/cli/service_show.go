package cli

import (
	"net/url"
	"strings"
)

// shown is value, of the variable or the flag named key, as an install
// prints it, for the credential it may hold. Two rules, in one function so
// that the text and --json pass the same: a list of the telemetry export's
// request headers (Authorization is among them) is hidden whole, as a
// setting named like a secret is, and the password of a URL's userinfo, a
// proxy's address or a collector's, is hidden as url.URL.Redacted hides it.
// The unit's file has the value as it is.
func shown(key, value string) string {
	if value != "" && otelHeaders(key) {
		return hidden(true)
	}
	return redactURL(value)
}

// redactURL is v with the password of its userinfo hidden (user:xxxxx@host),
// and v as it is when it is no URL with one. A proxy's address may leave the
// scheme out: Go and curl read user:secret@host:3128 as http://, which
// url.Parse does not.
func redactURL(v string) string {
	if u, err := url.Parse(v); err == nil && hasPassword(u) {
		return u.Redacted()
	}
	if u, err := url.Parse("http://" + v); err == nil && hasPassword(u) {
		return strings.TrimPrefix(u.Redacted(), "http://")
	}
	return v
}

// hasPassword reports whether the userinfo of u has a password, an empty
// one too.
func hasPassword(u *url.URL) bool {
	if u.User == nil {
		return false
	}
	_, ok := u.User.Password()
	return ok
}

// shownEnv is env, KEY=value each, as an install prints it (shown).
func shownEnv(env []string) []string {
	out := make([]string, len(env))
	for i, kv := range env {
		out[i] = kv
		if key, value, ok := strings.Cut(kv, "="); ok {
			out[i] = key + "=" + shown(key, value)
		}
	}
	return out
}

// shownArgs is args as an install prints them (shown): each word, with the
// flag before it for its key.
func shownArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		key := ""
		if i > 0 {
			key = args[i-1]
		}
		out[i] = shown(key, a)
	}
	return out
}
