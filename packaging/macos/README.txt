Trackz @VERSION@ for macOS (@ARCH@)
===================================

1. Drag Trackz to Applications.

2. Open Trackz (the viewer). It is not notarized by Apple: the first time,
   right-click it in Applications and choose Open, then Open again.

3. Start the recorder at login, from Terminal:

     cp /Applications/Trackz.app/Contents/Resources/org.solworktech.trackz.plist ~/Library/LaunchAgents/
     launchctl load -w ~/Library/LaunchAgents/org.solworktech.trackz.plist

4. Allow the recorder to read window titles: System Settings > Privacy &
   Security > Accessibility, add /Applications/Trackz.app/Contents/MacOS/trackz
   (in the file picker, Cmd+Shift+G and paste the path), then restart it:

     launchctl kickstart -k gui/$(id -u)/org.solworktech.trackz

Without step 4 trackz still records which application has focus, but not
the window titles.

More: https://github.com/solworktech/trackz
