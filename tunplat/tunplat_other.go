//go:build !windows && !darwin

package tunplat

// Up reports that this platform has no TUN backend; the UI hides the switch.
func Up(name string, mtu int) (*Platform, error) { return nil, ErrUnsupported }

// Recover has nothing to undo on a platform that never configured anything.
func Recover(tok map[string]string) error { return ErrUnsupported }

// Supported keeps the TUN switch out of the interface on Linux and the BSDs.
func Supported() bool { return false }
