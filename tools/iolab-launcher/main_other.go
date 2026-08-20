//go:build !windows && !darwin

package main

// Keep the pre-existing development build path available on Linux and other
// non-shipping hosts. The shipping targets use main_windows.go or
// main_darwin.go.
func main() {
	windowsMain()
}
