//go:build darwin

package processgroup

import "syscall"

// New returns process attributes that place a child in its own process group.
func New() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Chroot:     "",
		Credential: nil,
		Ptrace:     false,
		Setsid:     false,
		Setpgid:    true,
		Setctty:    false,
		Noctty:     false,
		Ctty:       0,
		Foreground: false,
		Pgid:       0,
	}
}
