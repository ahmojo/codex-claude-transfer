Output "13-markdown.gif"

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
Type "#  Turn a session into readable Markdown to share or keep"
Sleep 1s
Enter
Sleep 800ms

Type "cct export --session b2c3 --format md -o jwt-auth.md"
Sleep 400ms
Enter
Sleep 2400ms

Type "head -16 jwt-auth.md"
Sleep 400ms
Enter
Sleep 3400ms
