Output "08-cli-ui.gif"

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
Type "export CLAUDE_HOME=$HOME/cct-rec/_no_claude" Enter
Type "cd $HOME/cct-rec" Enter
Type "export PS1='$ '" Enter
Type "clear" Enter
Show

Sleep 800ms
Type "#  Too many flags? 'cct ui' is a guided menu that builds them for you"
Sleep 1s
Enter
Sleep 1s

Type "cct ui"
Sleep 400ms
Enter
Sleep 2500ms

# Menu order: Export, Import, Inspect, List, Doctor, Quit.
# Move down to Doctor and run it — note it echoes the real command first.
Down@550ms 4
Sleep 1200ms
Enter
Sleep 3500ms

# Back at the menu — move down to Quit and exit.
Down@550ms 5
Sleep 1000ms
Enter
Sleep 1500ms

Type "#  it printed the exact command it ran — copy it once you know it."
Enter
Sleep 2500ms
