package secrets

import (
	"strings"
	"testing"
)

func TestScanDetects(t *testing.T) {
	cases := map[string]string{
		"AWS access key":    "deploy with AKIAIOSFODNN7EXAMPLE today",
		"Anthropic API key": "key=sk-ant-api03-abcdefghij1234567890ABCD",
		"OpenAI API key":    "OPENAI=sk-abcdefghijklmnopqrst1234",
		"GitHub token":      "token ghp_0123456789abcdefghijklmnopqrstuvwxyz",
		"Private key block": "-----BEGIN OPENSSH PRIVATE KEY-----",
		"Google API key":    "AIzaabcdefghijklmnopqrstuvwxyz012345678",
	}
	for wantType, content := range cases {
		found := Scan([]byte(content))
		ok := false
		for _, f := range found {
			if f.Type == wantType {
				ok = true
				if strings.Contains(f.Masked, "EXAMPLE") && wantType == "AWS access key" {
					t.Errorf("%s: masked value still shows the middle: %q", wantType, f.Masked)
				}
			}
		}
		if !ok {
			t.Errorf("did not detect %s in %q (found=%+v)", wantType, content, found)
		}
	}
}

func TestScanCleanTextNoFalsePositive(t *testing.T) {
	clean := "Let's talk about how to build a rate limiter and a state machine in Go."
	if found := Scan([]byte(clean)); len(found) != 0 {
		t.Errorf("false positives on clean text: %+v", found)
	}
}

func TestRedactReplaces(t *testing.T) {
	content := []byte("my key AKIAIOSFODNN7EXAMPLE and token ghp_0123456789abcdefghijklmnopqrstuvwxyz")
	out, n := Redact(content)
	if n < 2 {
		t.Fatalf("redacted %d, want >= 2", n)
	}
	s := string(out)
	if strings.Contains(s, "AKIAIOSFODNN7EXAMPLE") || strings.Contains(s, "ghp_") {
		t.Errorf("secret survived redaction: %q", s)
	}
	if !strings.Contains(s, "[REDACTED:") {
		t.Errorf("no placeholder written: %q", s)
	}
}

func TestHasSecrets(t *testing.T) {
	if !HasSecrets([]byte("AKIAIOSFODNN7EXAMPLE")) {
		t.Error("HasSecrets missed an AWS key")
	}
	if HasSecrets([]byte("nothing secret here")) {
		t.Error("HasSecrets false positive")
	}
}
