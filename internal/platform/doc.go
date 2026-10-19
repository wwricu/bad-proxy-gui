// Package platform holds the code that has to be written twice, once per
// operating system.
//
// Everything here answers one of two questions, and both questions are about
// the shell rather than about proxying: where does this platform keep its files
// (%AppData% beside a `.portable` marker, or a macOS bundle's resource dir), and
// what does it let an app do to itself (set the system proxy, register a login
// item, shake a window, parent one window to another, come back from the Dock).
//
// The rule for adding to it is that every capability gets a file, and that file
// has one variant per supported OS named after the OS: sysproxy_windows.go and
// sysproxy_darwin.go. A capability that is a no-op on one platform still needs
// both files, because the caller should not have to know which one it is on.
//
// What does not belong here: the tray. It is per-platform too, but it is opened
// with the App struct and drives the window, so it lives beside them and reaches
// in here for the parts that fork.
package platform
