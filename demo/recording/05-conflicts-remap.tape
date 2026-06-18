Output "05-conflicts-remap.gif"

Set Shell "bash"
Set FontFamily "DejaVu Sans Mono"
Set FontSize 17
Set Width 1280
Set Height 860
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 42ms

Hide
Type "export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH" Enter
Type "export CODEX_HOME=$HOME/cct-rec/pc/codex-home" Enter
Type "cd $HOME/cct-rec" Enter
Type "export PS1='$ '" Enter
Type "clear" Enter
Show

Sleep 800ms
Type "#  A chat changed on BOTH machines — cct never overwrites silently"
Sleep 1s
Enter
Sleep 1s

Type "cct import sessions.codexbundle --dry-run"
Sleep 400ms
Enter
Sleep 4000ms

Type "#  Option A: overwrite, but keep a timestamped backup of mine"
Enter
Sleep 700ms
Type "cct import sessions.codexbundle --replace-with-backup"
Sleep 400ms
Enter
Sleep 3500ms

Hide
Type "bash $HOME/cct-rec/prep.sh conflict >/dev/null 2>&1" Enter
Type "export CODEX_HOME=$HOME/cct-rec/pc/codex-home" Enter
Type "clear" Enter
Show

Sleep 600ms
Type "#  Option B: keep BOTH — import the bundle's copy as a new session"
Enter
Sleep 700ms
Type "cct import sessions.codexbundle --import-as-copy"
Sleep 400ms
Enter
Sleep 3500ms

Hide
Type "rm -rf $HOME/cct-rec/pc2 && mkdir -p $HOME/cct-rec/pc2/codex-home" Enter
Type "export CODEX_HOME=$HOME/cct-rec/pc2/codex-home" Enter
Type "clear" Enter
Show

Sleep 600ms
Type "#  Different folder layout here? Redirect the project path on import:"
Enter
Sleep 700ms
Type "cct import sessions.codexbundle --map-cwd $HOME/projects/todo-api=$HOME/projects/todo-api-here"
Sleep 400ms
Enter
Sleep 4000ms
