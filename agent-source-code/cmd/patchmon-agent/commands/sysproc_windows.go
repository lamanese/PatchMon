//go:build windows

package commands

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// sysProcAttrForDetach detaches the child from this process (no console, own
// process group) so the restart helper survives when the service process exits.
// Setsid is Unix-only; on Windows detachment works via creation flags.
func sysProcAttrForDetach() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}
