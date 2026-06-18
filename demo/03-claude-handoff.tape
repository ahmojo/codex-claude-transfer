Output "03-claude-handoff.gif"

Set Shell "bash"
Set FontFamily "DejaVu Sans Mono"
Set FontSize 18
Set Width 1280
Set Height 820
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 45ms

Hide
Type "export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH" Enter
Type "export CLAUDE_HOME=$HOME/cct-rec/claude-home" Enter
Type "cd $HOME/cct-rec" Enter
Type "export PS1='$ '" Enter
Type "clear" Enter
Show

Sleep 800ms
Type "#  Cross-agent handoff: take a Codex bundle into Claude Code"
Sleep 1s
Enter
Sleep 1s

Type "#  translate the Codex sessions into Claude Code's own format"
Enter
Sleep 600ms
Type "cct import sessions.codexbundle --to claude"
Sleep 400ms
Enter
Sleep 4500ms

Type "#  they're now Claude Code sessions on this machine:"
Enter
Sleep 700ms
Type "cct doctor --tool claude"
Sleep 400ms
Enter
Sleep 3500ms

Type "#  open Claude Code and continue the conversation where Codex left off."
Enter
Sleep 2500ms
