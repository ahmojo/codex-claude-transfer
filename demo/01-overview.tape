Output "01-overview.gif"

Set Shell "bash"
Set FontFamily "DejaVu Sans Mono"
Set FontSize 18
Set Width 1280
Set Height 760
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 45ms

Hide
Type "export PATH=$HOME/.local/bin:$PATH" Enter
Type "export CODEX_HOME=$HOME/cct-rec/laptop/codex-home" Enter
Type "cd $HOME/cct-rec" Enter
Type "export PS1='$ '" Enter
Type "clear" Enter
Show

Sleep 800ms
Type "#  cct — carry your Codex & Claude Code sessions between machines"
Sleep 1s
Enter
Sleep 1s

Type "#  1) health check: does cct see this machine's sessions?"
Enter
Sleep 600ms
Type "cct doctor"
Sleep 400ms
Enter
Sleep 2500ms

Type "#  2) list — now grouped by project, newest first"
Enter
Sleep 600ms
Type "cct list"
Sleep 400ms
Enter
Sleep 4500ms

Type "#  3) pack everything into one portable file"
Enter
Sleep 600ms
Type "cct export --all -o sessions.codexbundle"
Sleep 400ms
Enter
Sleep 3000ms

Type "#  copy that one .codexbundle to the other machine — that's it."
Enter
Sleep 2500ms
