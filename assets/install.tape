# Gridctl Installation GIF
# Generates: install.gif

Output assets/install.gif

# Terminal settings - dark theme matching Obsidian Observatory
Set FontSize 14
Set Width 1200
Set Height 700
Set Theme "Catppuccin Mocha"
Set TypingSpeed 50ms
Set Padding 20
Set Margin 10
Set MarginFill "#08080a"
Set BorderRadius 8
Set WindowBar Colorful
Set WindowBarSize 40
Set LoopOffset 0%

Set Shell "zsh"

# Set a clean prompt with amber > using zsh prompt format and clear screen
# Using zsh's %F{color} with 256-color code 214 (closest to amber #f59e0b)
Hide
Type "PS1='%F{214}>%f ' && clear"
Enter
Sleep 200ms
Show

# Initial pause to let viewer orient
Sleep 500ms

# Tap the homebrew repo
Type "brew tap gridctl/gridctl"
Sleep 300ms
Enter
Sleep 4s

# Install gridctl
Type "brew install gridctl"
Sleep 300ms
Enter
Sleep 6s

# Show version
Type "gridctl version"
Sleep 300ms
Enter
Sleep 3s

# Final pause before loop
Sleep 2s
