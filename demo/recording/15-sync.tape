Output "15-sync.gif"

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
Type "#  Same Wi-Fi? Sync with no file — just a one-time code."
Sleep 1s
Enter
Sleep 800ms

Type "#  It's opt-in, because it sends data over your LAN:"
Enter
Sleep 500ms
Type "cct sync serve --port 7777"
Sleep 400ms
Enter
Sleep 2600ms

Type "#  Confirm with --i-understand — it waits and shows a code:"
Enter
Sleep 500ms
Type "cct sync serve --port 7777 --i-understand &"
Sleep 400ms
Enter
Sleep 3000ms

Hide
Type "kill %1 2>/dev/null" Enter
Show
Sleep 800ms
