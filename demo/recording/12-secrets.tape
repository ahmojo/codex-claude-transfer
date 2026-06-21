Output "12-secrets.gif"

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
Type "#  Before you share a chat — did you paste a key into it?"
Sleep 1s
Enter
Sleep 800ms

Type "cct scan"
Sleep 400ms
Enter
Sleep 3600ms

Type "#  Export with the secrets replaced by placeholders"
Enter
Sleep 600ms
Type "cct export --all --redact -o safe-to-share.codexbundle"
Sleep 400ms
Enter
Sleep 3200ms
