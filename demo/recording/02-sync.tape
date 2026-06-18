Output "02-sync.gif"

Set Shell "bash"
Set FontFamily "DejaVu Sans Mono"
Set FontSize 18
Set Width 1280
Set Height 820
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 45ms

Hide
Type "export PATH=$HOME/.local/bin:$PATH" Enter
Type "export CODEX_HOME=$HOME/cct-rec/pc/codex-home" Enter
Type "cd $HOME/cct-rec" Enter
Type "export PS1='$ '" Enter
Type "clear" Enter
Show

Sleep 800ms
Type "#  On the 2nd machine: incremental sync — add only what's new"
Sleep 1s
Enter
Sleep 1s

Type "#  preview first (--dry-run writes nothing)"
Enter
Sleep 600ms
Type "cct import sessions.codexbundle --merge --dry-run"
Sleep 400ms
Enter
Sleep 4500ms

Type "#  1 brand-new chat, 1 existing chat grew by 6 lines. Apply it:"
Enter
Sleep 800ms
Type "cct import sessions.codexbundle --merge"
Sleep 400ms
Enter
Sleep 3500ms

Type "#  run the SAME bundle again — nothing to do, totally safe"
Enter
Sleep 800ms
Type "cct import sessions.codexbundle --merge"
Sleep 400ms
Enter
Sleep 3500ms

Type "#  append-only + lossless: your chats are never re-pasted or overwritten."
Enter
Sleep 2500ms
