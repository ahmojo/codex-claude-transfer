// Package meta stores cct's own per-session annotations — a friendly name and a
// set of tags — keyed by the session's thread id. Coding agents give you no way
// to organize a growing pile of sessions; this is that organizational layer.
//
// The annotations live in a single JSON file (meta.json) under cct's config dir.
// cct NEVER writes them into the agent's session files or its index: this sidecar
// is purely cct's, so tagging a session can never corrupt or slow down the agent.
package meta

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileName is the sidecar file's name within the config dir.
const FileName = "meta.json"

// Entry is the annotation for one session.
type Entry struct {
	Name string   `json:"name,omitempty"`
	Tags []string `json:"tags,omitempty"`
}

// Store is the full set of annotations, keyed by thread id.
type Store struct {
	Sessions map[string]Entry `json:"sessions"`
}

// FilePath returns the sidecar path within dir.
func FilePath(dir string) string { return filepath.Join(dir, FileName) }

// Load reads the sidecar from dir. A missing file yields an empty store.
func Load(dir string) (*Store, error) {
	s := &Store{Sessions: map[string]Entry{}}
	data, err := os.ReadFile(FilePath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return s, fmt.Errorf("meta %s is not valid JSON: %w", FilePath(dir), err)
	}
	if s.Sessions == nil {
		s.Sessions = map[string]Entry{}
	}
	return s, nil
}

// Save writes the sidecar to dir (0600), creating the directory if needed.
func (s *Store) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(FilePath(dir), append(data, '\n'), 0o600)
}

// Get returns the annotation for a thread id (zero Entry when none).
func (s *Store) Get(id string) Entry { return s.Sessions[id] }

// SetName sets (or, with an empty name, clears) a session's friendly name.
func (s *Store) SetName(id, name string) {
	e := s.Sessions[id]
	e.Name = strings.TrimSpace(name)
	s.put(id, e)
}

// AddTag adds a normalized tag, returning false if it was already present.
func (s *Store) AddTag(id, tag string) bool {
	tag = NormalizeTag(tag)
	if tag == "" {
		return false
	}
	e := s.Sessions[id]
	for _, t := range e.Tags {
		if t == tag {
			return false
		}
	}
	e.Tags = append(e.Tags, tag)
	sort.Strings(e.Tags)
	s.put(id, e)
	return true
}

// RemoveTag removes a tag, returning false if it was not present.
func (s *Store) RemoveTag(id, tag string) bool {
	tag = NormalizeTag(tag)
	e := s.Sessions[id]
	out := e.Tags[:0:0]
	removed := false
	for _, t := range e.Tags {
		if t == tag {
			removed = true
			continue
		}
		out = append(out, t)
	}
	if !removed {
		return false
	}
	e.Tags = out
	s.put(id, e)
	return true
}

// HasTag reports whether a session carries the given tag.
func (s *Store) HasTag(id, tag string) bool {
	tag = NormalizeTag(tag)
	for _, t := range s.Sessions[id].Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// IDsWithTag returns the thread ids carrying the given tag, sorted.
func (s *Store) IDsWithTag(tag string) []string {
	tag = NormalizeTag(tag)
	var out []string
	for id := range s.Sessions {
		if s.HasTag(id, tag) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// AllTags returns every distinct tag in use, sorted.
func (s *Store) AllTags() []string {
	seen := map[string]bool{}
	for _, e := range s.Sessions {
		for _, t := range e.Tags {
			seen[t] = true
		}
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// put stores e, or deletes the key when the entry has become empty, so the
// sidecar does not accumulate empty records.
func (s *Store) put(id string, e Entry) {
	if e.Name == "" && len(e.Tags) == 0 {
		delete(s.Sessions, id)
		return
	}
	s.Sessions[id] = e
}

// NormalizeTag lowercases and trims a tag and collapses internal whitespace to a
// single dash, so "In Progress" and "in-progress" are the same tag.
func NormalizeTag(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	return strings.Join(strings.Fields(tag), "-")
}
