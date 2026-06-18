Output "09-compressed.gif"

Set Shell "bash"
Set FontFamily "DejaVu Sans Mono"
Set FontSize 17
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
Type "#  Codex stores older sessions compressed (.jsonl.zst)"
Sleep 1s
Enter
Sleep 1s

Type "#  with zstd installed, cct reads them just like plain ones"
Enter
Sleep 600ms
Type "cct doctor"
Sleep 400ms
Enter
Sleep 3500ms

Type "#  the compressed session still lists with its preview + a [compressed] tag"
Enter
Sleep 700ms
Type "cct list"
Sleep 400ms
Enter
Sleep 4500ms
