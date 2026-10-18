//go:build darwin

// Wails' darwin frontend imports UTType (behind a __has_include guard, so it
// only shows up with a recent SDK) but never names the framework that defines
// it, and the final link dies on "_OBJC_CLASS_$_UTType". Nothing of ours
// refers to it, so the missing flag has to be added from here. This file only
// carries that flag, which is why there is no other code in it.
package main

/*
#cgo LDFLAGS: -framework UniformTypeIdentifiers
*/
import "C"
