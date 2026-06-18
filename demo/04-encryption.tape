Output "04-encryption.gif"

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
Type "PUB=$(age-keygen -y key.txt)" Enter
Type "clear" Enter
Show

Sleep 800ms
Type "#  Encrypt a bundle with age (end-to-end, your keys only)"
Sleep 1s
Enter
Sleep 1s

Type "#  encrypt to a public key — plaintext is removed automatically"
Enter
Sleep 600ms
Type "cct export --all --encrypt-to $PUB -o secret.codexbundle"
Sleep 400ms
Enter
Sleep 3500ms

Type "#  only the .age file exists now:"
Enter
Sleep 600ms
Type "ls secret.codexbundle*"
Sleep 400ms
Enter
Sleep 2500ms

Type "#  read it back with your private key file"
Enter
Sleep 700ms
Type "cct inspect secret.codexbundle.age --identity key.txt"
Sleep 400ms
Enter
Sleep 4000ms
