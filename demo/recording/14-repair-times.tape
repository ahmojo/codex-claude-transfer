Output "14-repair-times.gif"

Set Shell "bash"
Set FontFamily "DejaVu Sans Mono"
Set FontSize 18
Set Width 1280
Set Height 900
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
Type "#  Imported sessions opening slowly? doctor spots why"
Sleep 1s
Enter
Sleep 800ms

Type "cct doctor"
Sleep 400ms
Enter
Sleep 3600ms

Type "#  One-time fix — only file timestamps, never content or the index"
Enter
Sleep 600ms
Type "cct repair-times --dry-run"
Sleep 400ms
Enter
Sleep 2800ms

Type "cct repair-times"
Sleep 400ms
Enter
Sleep 2800ms
