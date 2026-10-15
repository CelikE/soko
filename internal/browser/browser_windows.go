package browser

import "golang.org/x/sys/windows"

var shellExecute = windows.ShellExecute

func openURL(rawURL string) error {
	target, err := windows.UTF16PtrFromString(rawURL)
	if err != nil {
		return err
	}
	// Let Windows open the URL directly; cmd.exe would interpret URL characters
	// such as &, |, ^, and % as shell syntax rather than literal URL content.
	return shellExecute(0, nil, target, nil, nil, windows.SW_SHOWNORMAL)
}
