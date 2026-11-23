package ai

import "regexp"

var (
	pemBlockRe  = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	tokenPrefRe = regexp.MustCompile(`\b(sk-[A-Za-z0-9_-]{16,}|gh[pousr]_[A-Za-z0-9]{20,}|AKIA[0-9A-Z]{16}|tskey-[A-Za-z0-9_-]{12,}|eyJ[A-Za-z0-9_-]{15,}\.[A-Za-z0-9_-]{15,}\.[A-Za-z0-9_-]{10,})\b`)
	bearerRe    = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._\-]{16,}`)
	envSecretRe = regexp.MustCompile(`(?i)\b(password|passwd|secret|api_key|apikey|access_token|auth_token|private_key)(\s*[:=]\s*)([^\s"';]+)`)
)

// RedactSecrets masks suspected private keys, API keys, JWTs, Bearer tokens, and secret assignments
// before terminal context or command output is sent to an external LLM provider.
func RedactSecrets(s string) string {
	if s == "" {
		return ""
	}
	out := pemBlockRe.ReplaceAllString(s, "[REDACTED_PRIVATE_KEY]")
	out = tokenPrefRe.ReplaceAllString(out, "[REDACTED_TOKEN]")
	out = bearerRe.ReplaceAllString(out, "${1}[REDACTED_TOKEN]")
	out = envSecretRe.ReplaceAllString(out, "${1}${2}[REDACTED]")
	return out
}
