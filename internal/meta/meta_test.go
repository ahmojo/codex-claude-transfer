package meta

import (
	"reflect"
	"testing"
)

func TestTagsAndNameRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	s.SetName("id1", "auth refactor")
	if !s.AddTag("id1", "WIP") {
		t.Fatal("AddTag should report added")
	}
	if s.AddTag("id1", "wip") {
		t.Fatal("duplicate (normalized) tag should report not-added")
	}
	s.AddTag("id1", "review")
	if err := s.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	e := got.Get("id1")
	if e.Name != "auth refactor" {
		t.Fatalf("name = %q", e.Name)
	}
	if !reflect.DeepEqual(e.Tags, []string{"review", "wip"}) { // sorted
		t.Fatalf("tags = %v", e.Tags)
	}
}

func TestRemoveTagAndPrune(t *testing.T) {
	s := &Store{Sessions: map[string]Entry{}}
	s.AddTag("id", "x")
	if !s.RemoveTag("id", "x") {
		t.Fatal("RemoveTag should report removed")
	}
	if s.RemoveTag("id", "x") {
		t.Fatal("removing absent tag should report not-removed")
	}
	// Entry became empty -> pruned.
	if _, ok := s.Sessions["id"]; ok {
		t.Fatal("empty entry should be pruned")
	}
}

func TestNormalizeTag(t *testing.T) {
	cases := map[string]string{
		"  In Progress ": "in-progress",
		"WIP":            "wip",
		"a   b":          "a-b",
		"":               "",
	}
	for in, want := range cases {
		if got := NormalizeTag(in); got != want {
			t.Errorf("NormalizeTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIDsWithTagAndAllTags(t *testing.T) {
	s := &Store{Sessions: map[string]Entry{}}
	s.AddTag("b", "ship")
	s.AddTag("a", "ship")
	s.AddTag("a", "wip")
	if got := s.IDsWithTag("ship"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("IDsWithTag = %v", got)
	}
	if got := s.AllTags(); !reflect.DeepEqual(got, []string{"ship", "wip"}) {
		t.Fatalf("AllTags = %v", got)
	}
}
