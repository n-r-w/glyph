//go:build linux

package processgroup

import "syscall"

// New returns process attributes that place a child in its own process group.
func New() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Chroot:                     "",
		Credential:                 nil,
		Ptrace:                     false,
		Setsid:                     false,
		Setpgid:                    true,
		Setctty:                    false,
		Noctty:                     false,
		Ctty:                       0,
		Foreground:                 false,
		Pgid:                       0,
		Pdeathsig:                  0,
		Cloneflags:                 0,
		Unshareflags:               0,
		UidMappings:                nil,
		GidMappings:                nil,
		GidMappingsEnableSetgroups: false,
		AmbientCaps:                nil,
		UseCgroupFD:                false,
		CgroupFD:                   0,
		PidFD:                      nil,
	}
}
