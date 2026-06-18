Output "06-export-filters.gif"

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
Type "export CODEX_HOME=$HOME/cct-rec/laptop/codex-home" Enter
Type "cd $HOME/cct-rec" Enter
Type "export PS1='$ '" Enter
Type "clear" Enter
Show

Sleep 800ms
Type "#  Export exactly what you need — not always everything"
Sleep 1s
Enter
Sleep 1s

Type "#  just one project folder"
Enter
Sleep 600ms
Type "cct export --project $HOME/projects/todo-api -o todo-api.codexbundle"
Sleep 400ms
Enter
Sleep 2800ms

Type "#  a single session by thread-id prefix"
Enter
Sleep 600ms
Type "cct export --session b2c3 -o one-session.codexbundle"
Sleep 400ms
Enter
Sleep 2800ms

Type "#  only what changed recently (date or 7d / 48h)"
Enter
Sleep 600ms
Type "cct export --all --since 2026-06-12 -o recent.codexbundle"
Sleep 400ms
Enter
Sleep 3000ms
