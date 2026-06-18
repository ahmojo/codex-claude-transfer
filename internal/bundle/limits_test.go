package bundle

import (
	"archive/zip"
	"strings"
	"testing"
)

func TestReadCapped(t *testing.T) {
	if _, err := readCapped(strings.NewReader("hello"), 100, "x"); err != nil {
		t.Fatalf("within limit: %v", err)
	}
	if _, err := readCapped(strings.NewReader("abc"), 3, "x"); err != nil {
		t.Fatalf("exactly at limit should be ok: %v", err)
	}
	if _, err := readCapped(strings.NewReader("toolong"), 3, "x"); err == nil {
		t.Fatal("over-limit read should error (bomb guard)")
	}
}

func TestCheckDeclaredSize(t *testing.T) {
	over := &zip.File{FileHeader: zip.FileHeader{Name: "big", UncompressedSize64: uint64(MaxSessionBytes) + 1}}
	if err := checkDeclaredSize(over, MaxSessionBytes, "big"); err == nil {
		t.Fatal("declared-oversize entry should be rejected")
	}
	ok := &zip.File{FileHeader: zip.FileHeader{Name: "ok", UncompressedSize64: 10}}
	if err := checkDeclaredSize(ok, MaxSessionBytes, "ok"); err != nil {
		t.Fatalf("normal entry should pass: %v", err)
	}
}
