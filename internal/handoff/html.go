package handoff

import (
	"fmt"
	"html"
	"strings"
)

// ToHTML renders a parsed session as a self-contained, styled HTML document for
// reading or sharing a conversation outside the agent. Like ToMarkdown it is NOT
// re-importable. Every piece of session-derived text is HTML-escaped, so a
// conversation that itself contains markup or a "</script>" cannot break out of
// the page or inject script — important because sessions are untrusted content.
func ToHTML(s AgentSession) []byte {
	var b strings.Builder

	title := strings.TrimSpace(firstUserLine(s))
	if title == "" {
		title = "Session"
	}

	b.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", esc(title))
	b.WriteString(htmlStyle)
	b.WriteString("</head>\n<body>\n<main>\n")

	fmt.Fprintf(&b, "<h1>%s</h1>\n", esc(title))

	b.WriteString("<dl class=\"meta\">\n")
	metaRow(&b, "Agent", s.SourceAgent)
	metaRow(&b, "Project", s.CWD)
	metaRow(&b, "Created", s.CreatedAt)
	if s.Git != nil {
		metaRow(&b, "Git branch", s.Git.Branch)
		metaRow(&b, "Git commit", s.Git.Commit)
	}
	b.WriteString("</dl>\n")

	for _, t := range s.Conversation {
		switch t.Role {
		case RoleUser:
			turnBlock(&b, "user", "🧑 User", t.Text, false)
		case RoleAssistant:
			turnBlock(&b, "assistant", "🤖 Assistant", t.Text, false)
		case RoleTool:
			name := t.Tool
			if name == "" {
				name = "tool"
			}
			turnBlock(&b, "tool", "🔧 "+name, t.Text, true)
		}
	}

	b.WriteString("</main>\n</body>\n</html>\n")
	return []byte(b.String())
}

// turnBlock writes one conversation turn. When pre is true the body is rendered
// in a <pre> (for tool/command output); otherwise paragraphs are preserved.
func turnBlock(b *strings.Builder, class, label, text string, pre bool) {
	fmt.Fprintf(b, "<section class=\"turn %s\">\n<h2>%s</h2>\n", class, esc(label))
	text = strings.TrimRight(text, "\n")
	if text == "" {
		b.WriteString("<p class=\"empty\">(empty)</p>\n")
	} else if pre {
		fmt.Fprintf(b, "<pre>%s</pre>\n", esc(text))
	} else {
		for _, para := range strings.Split(text, "\n\n") {
			para = strings.TrimRight(para, "\n")
			if strings.TrimSpace(para) == "" {
				continue
			}
			// Keep single newlines inside a paragraph as <br> for readability.
			fmt.Fprintf(b, "<p>%s</p>\n", strings.ReplaceAll(esc(para), "\n", "<br>\n"))
		}
	}
	b.WriteString("</section>\n")
}

func metaRow(b *strings.Builder, label, val string) {
	if strings.TrimSpace(val) == "" {
		return
	}
	fmt.Fprintf(b, "<dt>%s</dt><dd>%s</dd>\n", esc(label), esc(val))
}

// esc HTML-escapes text from the (untrusted) session.
func esc(s string) string { return html.EscapeString(s) }

const htmlStyle = `<style>
:root { color-scheme: light dark; }
* { box-sizing: border-box; }
body { margin: 0; background: #f5f5f7; color: #1d1d1f; font: 16px/1.6 -apple-system, Segoe UI, Roboto, Helvetica, Arial, sans-serif; }
main { max-width: 820px; margin: 0 auto; padding: 2rem 1.25rem 4rem; }
h1 { font-size: 1.6rem; line-height: 1.3; }
dl.meta { display: grid; grid-template-columns: max-content 1fr; gap: .15rem 1rem; margin: 0 0 2rem; padding: 1rem 1.25rem; background: #fff; border-radius: 10px; font-size: .9rem; }
dl.meta dt { color: #6e6e73; }
dl.meta dd { margin: 0; word-break: break-word; }
section.turn { background: #fff; border-radius: 10px; padding: 1rem 1.25rem; margin: 0 0 1rem; box-shadow: 0 1px 2px rgba(0,0,0,.05); }
section.turn h2 { font-size: .8rem; text-transform: uppercase; letter-spacing: .04em; color: #6e6e73; margin: 0 0 .5rem; }
section.user { border-left: 3px solid #0a84ff; }
section.assistant { border-left: 3px solid #34c759; }
section.tool { border-left: 3px solid #8e8e93; }
section.turn p { margin: 0 0 .75rem; white-space: normal; }
section.turn p:last-child { margin-bottom: 0; }
section.turn pre { margin: 0; padding: .75rem; overflow-x: auto; background: #1d1d1f; color: #f5f5f7; border-radius: 8px; font: .85rem/1.5 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
.empty { color: #aaa; font-style: italic; }
@media (prefers-color-scheme: dark) {
  body { background: #000; color: #f5f5f7; }
  dl.meta, section.turn { background: #1c1c1e; }
  dl.meta dt, section.turn h2 { color: #98989d; }
}
</style>
`
