package git

import "testing"

func TestValidateRemoteURL(t *testing.T) {
	allowed := []string{
		"https://github.com/o/r.git",
		"http://host/r",
		"ssh://git@host/r",
		"git://host/r",
		"git@github.com:o/r.git",
		"file:///srv/repo.git", // file/local are not command-execution vectors
		"/srv/mirror/repo.git", // local path (e.g. a local mirror)
		`C:\Users\me\repo.git`, // Windows local path
	}
	for _, r := range allowed {
		if err := validateRemoteURL(r); err != nil {
			t.Errorf("%q should be allowed: %v", r, err)
		}
	}
	rejected := []string{
		"ext::sh -c 'evil'",   // command-execution remote helper
		"fd::17",              // remote helper
		"gcrypt::https://x",   // any name::addr helper transport
		"-oProxyCommand=evil", // looks like a flag
		"",
	}
	for _, r := range rejected {
		if err := validateRemoteURL(r); err == nil {
			t.Errorf("%q should be rejected", r)
		}
	}
}

func TestHexOID(t *testing.T) {
	if !hexOIDRe.MatchString("deadbeef") {
		t.Error("a hex sha should match")
	}
	if !hexOIDRe.MatchString("0123456789abcdef0123456789abcdef01234567") {
		t.Error("a 40-char sha should match")
	}
	for _, bad := range []string{"--evil", "zzz", "", "HEAD", "-rf"} {
		if hexOIDRe.MatchString(bad) {
			t.Errorf("%q should not be accepted as a commit", bad)
		}
	}
}
