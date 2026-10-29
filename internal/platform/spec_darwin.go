//go:build darwin

package platform

// TrayCanHide is true here too: the menu-bar item may be turned off. It used to
// be the only way back into a hidden window, which is why no toggle was offered
// at all - but launching the app again reaches the instance that is already
// running (SingleInstance) and shows its window, with or without a glyph on the
// bar. So closing the window is safe either way: it stays a hide here, where
// Windows turns it into a quit because its icon was the only way back.
const TrayCanHide = true

// TrayIsMenuBar names the item the way this platform does, so the settings
// window can say "menu bar" here and "tray" on Windows.
const TrayIsMenuBar = true

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

// MainWindowChromeH is the caption, once: the creation Height is a content rect
// but the minimum is enforced as a frame, so both need the same 31 px added or
// the window drags a caption below its own floor. Points are logical already -
// no DPI headroom - and the self-test reads the real page back.
const MainWindowChromeH = ResizeFrameHeight

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
