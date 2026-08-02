Output "16-skill.gif"

Set Shell "bash"
Set FontFamily "DejaVu Sans Mono"
Set FontSize 18
Set Width 1280
Set Height 820
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms

Hide
Type "export PATH=$HOME/.local/bin:$PATH" Enter
Type "export CLAUDE_HOME=$HOME/cct-rec/skill/claude-a" Enter
Type "export CCT_CONFIG_DIR=$HOME/cct-rec/skill/cfg" Enter
Type "cd $HOME/projects/todo-api" Enter
Type "export PS1='$ '" Enter
Type "clear" Enter
Show

Sleep 800ms
Type "#  Teach the agent the workflow — one file in your Claude Code home"
Enter
Sleep 600ms
Type "cct skill install"
Sleep 300ms
Enter
Sleep 3000ms

Type "#  Chat history goes to a PRIVATE store repo, not into this project"
Enter
Sleep 600ms
Type "cct config set repo-sync-repo ~/cct-rec/skill/sessions.git"
Sleep 300ms
Enter
Sleep 1500ms

Type "cct skill init"
Sleep 300ms
Enter
Sleep 3000ms

Type "#  That is all this repo carries — a reference, no transcripts"
Enter
Sleep 600ms
Type "cat .cct/sessions.json"
Sleep 300ms
Enter
Sleep 3500ms

Type "git add .cct && git commit -qm 'Point at the session store' && git push -q"
Sleep 300ms
Enter
Sleep 1500ms

Type "#  Save this project's sessions into the store, then commit them there"
Enter
Sleep 600ms
Type "cct export --project . --tool claude -o ~/cct-store/projects/todo-api/claude/claude-all.codexbundle"
Sleep 300ms
Enter
Sleep 3000ms

Type "git -C ~/cct-store add -A && git -C ~/cct-store commit -qm 'Update todo-api sessions' && git -C ~/cct-store push -q"
Sleep 300ms
Enter
Sleep 2000ms

Type "clear"
Enter
Sleep 500ms

Type "#  ── Second machine: clone the project, and it knows where its chats are"
Enter
Sleep 800ms
Hide
Type "export CLAUDE_HOME=$HOME/cct-rec/skill/claude-b" Enter
Type "clear" Enter
Show
Type "git clone -q ~/cct-rec/skill/todo-api.git ~/projects/todo-api-clone && cd ~/projects/todo-api-clone"
Sleep 300ms
Enter
Sleep 1500ms

Type "cct skill show"
Sleep 300ms
Enter
Sleep 4000ms

Type "git clone -q ~/cct-rec/skill/sessions.git ~/cct-store-b"
Sleep 300ms
Enter
Sleep 1500ms

Type "cct import ~/cct-store-b/projects/todo-api/claude/claude-all.codexbundle --merge --map-cwd-here"
Sleep 300ms
Enter
Sleep 4000ms

Type "cct list --tool claude"
Sleep 300ms
Enter
Sleep 4000ms

Type "#  Same chats, new machine, new path — and the code repo stayed clean."
Enter
Sleep 2500ms
