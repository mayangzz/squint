Output "stock.mp4"
Set Shell "bash"
Set FontSize 15
Set Width 1200
Set Height 760
Set Padding 16
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms
Hide
Type "source scripts/demo/env.sh"
Enter
Sleep 1s
Show
Sleep 500ms
Type "claude"
Sleep 300ms
Enter
Sleep 5s
Type "Run go vet ./..., then go build ./..., then git log --stat -3, then ls -laR internal, each as a separate command, then tell me in two sentences whether anything looks wrong."
Sleep 500ms
Enter
Sleep 50s
Hide
Ctrl+C
Ctrl+C
Sleep 1s
