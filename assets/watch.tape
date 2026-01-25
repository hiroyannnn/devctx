# VHS tape file for devctx watch mode demo
Output assets/watch.gif

Set Shell "zsh"
Set FontSize 14
Set Width 1200
Set Height 700
Set Theme "Catppuccin Mocha"

Type "devctx list -w"
Sleep 500ms
Enter
Sleep 2s

# Navigate with j/k
Type "j"
Sleep 500ms
Type "j"
Sleep 500ms
Type "j"
Sleep 500ms
Type "k"
Sleep 500ms

# Jump to top/bottom
Type "G"
Sleep 1s
Type "g"
Sleep 1s

# Quit
Type "q"
Sleep 500ms
