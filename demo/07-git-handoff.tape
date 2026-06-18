Output "07-git-handoff.gif"

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
Type "export CODEX_HOME=$HOME/cct-rec/gitsrc/codex-home" Enter
Type "cd $HOME/cct-rec" Enter
Type "export PS1='$ '" Enter
Type "clear" Enter
Show

Sleep 800ms
Type "#  Carry the chat AND a pointer to the matching code commit"
Sleep 1s
Enter
Sleep 1s

Type "#  record the project's git remote + commit inside the bundle"
Enter
Sleep 600ms
Type "cct export --project $HOME/cct-rec/gitdemo/work --with-git -o proj.codexbundle"
Sleep 400ms
Enter
Sleep 3000ms

Type "#  the bundle now knows which remote/commit it belongs to:"
Enter
Sleep 600ms
Type "cct inspect proj.codexbundle"
Sleep 400ms
Enter
Sleep 4000ms

Hide
Type "export CODEX_HOME=$HOME/cct-rec/pc2/codex-home" Enter
Type "rm -rf $HOME/cct-rec/pc2 $HOME/cct-rec/cloned && mkdir -p $HOME/cct-rec/pc2/codex-home" Enter
Type "clear" Enter
Show

Sleep 600ms
Type "#  on the other machine: import the chat AND clone the code"
Enter
Sleep 700ms
Type "cct import proj.codexbundle --clone $HOME/cct-rec/cloned"
Sleep 400ms
Enter
Sleep 4000ms
