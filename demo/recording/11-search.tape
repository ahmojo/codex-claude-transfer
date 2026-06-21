Output "11-search.gif"

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
Type "#  Which session was that idea in? Search your chats."
Sleep 1s
Enter
Sleep 800ms

Type "cct search 'JWT auth'"
Sleep 400ms
Enter
Sleep 3200ms

Type "#  regex, scoped to the last 30 days"
Enter
Sleep 600ms
Type "cct search 'cache|retry' --regex --since 30d"
Sleep 400ms
Enter
Sleep 3200ms

Type "#  ...then bundle only the sessions about a topic"
Enter
Sleep 600ms
Type "cct export --match 'auth' -o auth-sessions.codexbundle"
Sleep 400ms
Enter
Sleep 3000ms
