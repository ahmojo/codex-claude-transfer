// Package secrets detects (and optionally redacts) likely credentials in session
// content — API keys, tokens, private keys — so you can check a session before
// sharing or syncing it. Detection is best-effort and pattern-based: it favours
// high-signal patterns to keep false positives low, and never phones home.
package secrets

import (
	"fmt"
	"regexp"
	"strings"
)

// Finding is one detected secret, with the value masked for safe display.
type Finding struct {
	Type   string // human label, e.g. "AWS access key"
	Masked string // the match with the middle replaced by ***
}

type pattern struct {
	name string
	re   *regexp.Regexp
}

// patterns are ordered most-specific first. Each is anchored on a distinctive
// prefix/shape to minimise false positives.
var patterns = []pattern{
	{"Private key block", regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA |PGP )?PRIVATE KEY-----`)},
	{"AWS access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"GitHub token", regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36}\b`)},
	{"GitHub fine-grained token", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{82}\b`)},
	{"Anthropic API key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9-]{20,}`)},
	{"OpenAI API key", regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9]{20,}`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z\-_]{35}\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}`)},
	{"Bearer token", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._\-]{20,}`)},
	{"Generic secret assignment", regexp.MustCompile(`(?i)\b(?:api[_-]?key|secret|token|passwd|password)\b\s*[:=]\s*['"]?[A-Za-z0-9._/+\-]{16,}['"]?`)},
}

// Scan returns the distinct secrets found in content (masked). Duplicates of the
// same masked value+type are collapsed.
func Scan(content []byte) []Finding {
	s := string(content)
	seen := map[string]bool{}
	var out []Finding
	for _, p := range patterns {
		for _, m := range p.re.FindAllString(s, -1) {
			masked := mask(m)
			key := p.name + "|" + masked
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Finding{Type: p.name, Masked: masked})
		}
	}
	return out
}

// HasSecrets is a quick yes/no.
func HasSecrets(content []byte) bool {
	for _, p := range patterns {
		if p.re.Match(content) {
			return true
		}
	}
	return false
}

// Redact replaces every detected secret with a typed placeholder and reports how
// many were replaced. It is lossy and opt-in. Private-key blocks are replaced only
// at their header (the body is left, since the header alone signals the secret and
// rewriting multi-line PEM bodies risks corrupting structure); callers that need
// full-body redaction should drop the whole value.
func Redact(content []byte) ([]byte, int) {
	out := content
	total := 0
	for _, p := range patterns {
		out = p.re.ReplaceAllFunc(out, func(b []byte) []byte {
			total++
			return []byte(fmt.Sprintf("[REDACTED:%s]", strings.ReplaceAll(p.name, " ", "-")))
		})
	}
	return out, total
}

// mask shows the first 4 and last 2 characters, hiding the middle.
func mask(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 8 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + strings.Repeat("*", 6) + s[len(s)-2:]
}
