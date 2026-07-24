package safety

import "testing"

// TestIsArchivedSessionEntry documents the exact archived rollout layout that
// may be restored during an explicit archived-session relocation.
func TestIsArchivedSessionEntry(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "flat JSONL rollout",
			path: "archived_sessions/rollout-example.jsonl",
			want: true,
		},
		{
			name: "compressed rollout",
			path: "archived_sessions/rollout-example.jsonl.zst",
			want: true,
		},
		{
			name: "dated rollout",
			path: "archived_sessions/2026/07/24/rollout-example.jsonl",
			want: true,
		},
		{
			name: "active rollout",
			path: "sessions/2026/07/24/rollout-example.jsonl",
			want: false,
		},
		{
			name: "partial date directory",
			path: "archived_sessions/2026/rollout-example.jsonl",
			want: false,
		},
		{
			name: "nested rollout",
			path: "archived_sessions/2026/07/24/nested/rollout-example.jsonl",
			want: false,
		},
		{
			name: "non-rollout file",
			path: "archived_sessions/2026/07/24/session-example.jsonl",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsArchivedSessionEntry(tt.path); got != tt.want {
				t.Errorf("IsArchivedSessionEntry(%q) = %t, want %t", tt.path, got, tt.want)
			}
		})
	}
}
