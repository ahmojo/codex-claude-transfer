package sessions

import (
	"strings"
	"testing"
)

func TestPaginatedPreviewAndHistoryBase(t *testing.T) {
	meta, _ := parseRolloutReader(strings.NewReader(
		`{"type":"session_meta","payload":{"id":"fork","history_base":{"thread_id":"parent"}}}` + "\n" +
			`{"type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"instructions"}]}}` + "\n" +
			`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}`))
	if meta.HistoryBaseID != "parent" || meta.FirstUserMessage != "hello" || meta.Preview != "hello" {
		t.Fatalf("paginated metadata: %+v", meta)
	}
}
