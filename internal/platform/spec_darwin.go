//go:build darwin

package platform

// TrayCanHide is false on macOS: the menu-bar item is the only reopen
// affordance once the window is closed and the Dock icon is dropped, so the
// settings window hides the toggle rather than offering a control that cannot
// take effect.
const TrayCanHide = false

// WindowFrameHeight is 0 on macOS: Wails hands the window's Height straight to
// -[NSWindow initWithContentRect:...], so the number is already the webview's
// own height and the title bar is extra on top of it. Measured in the self-test
// run, which reads 452 px of page out of a window of 452 and a frame of 483.
const WindowFrameHeight = 0

// ResizeFrameHeight is 31 here, and it is not the same convention as the one
// above. -[WebviewWindow setSize:] ends in -[NSWindow setFrame:display:animate:]
// - the whole window, title bar included - where the creation call above took
// the content rect. So the number that opened a 455 px page is a 486 px window,
// and a form being resized has to pay the caption on top.
//
// 31 is what the self-test measured twice on the CI runner (a 486 frame over a
// 455 page, a 316 frame over a 285 one) rather than AppKit's nominal 28; the
// three pixels of difference are why the page reads its window back and asks
// again instead of trusting this.
const ResizeFrameHeight = 31

// The two forms' client heights: what each window opens at, before the page has
// measured itself and asked for the height it actually needs.
//
// They are not the Windows numbers: a text field and a popup are drawn by the
// operating system, and AppKit draws them shorter than Win32 does. The settings
// form is 47 px of content shorter here than on Windows and the node editor only
// 16 px, which is why a height shared between the two platforms leaves one of
// them either scrolling or holding a band of empty window under its own buttons.
const (
	NodeFormHeight     = 462
	SettingsFormHeight = 346
)
