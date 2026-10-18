//go:build darwin

package main

// Wails' darwin frontend imports UTType (behind a __has_include guard, so it
// only appears with a recent SDK) but its cgo flags never name the framework
// that defines it, and the final link dies on "_OBJC_CLASS_$_UTType". Nothing
// of ours refers to it, so the missing flag has to be added here.
/*
#cgo LDFLAGS: -framework UniformTypeIdentifiers
*/
import "C"
