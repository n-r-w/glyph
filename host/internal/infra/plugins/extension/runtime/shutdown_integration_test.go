//go:build integration

package runtime

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

// invocationTransportFailureText includes peer-owned text that matches the Host classification.
const invocationTransportFailureText = "complete invocation transport failure: " +
	"extension runtime unavailable; original peer cause Ω"

// TestManagerFailureReturnsBeforeRuntimeShutdown checks worker return and owner cleanup through a real process.
func TestManagerFailureReturnsBeforeRuntimeShutdown(t *testing.T) {
	const shutdownMonitoredNameTemplate = "monitored=%t"

	t.Parallel()

	// Arrange failure routes with and without process observation.
	for _, monitored := range []bool{false, true} {
		t.Run(fmt.Sprintf(shutdownMonitoredNameTemplate, monitored), func(t *testing.T) {
			t.Parallel()
			for _, invocation := range []string{
				shutdownToolInvocation, shutdownHandlerInvocation, shutdownLifecycleInvocation, shutdownCompactionInvocation,
			} {
				t.Run(invocation, func(t *testing.T) {
					t.Parallel()
					modes := []string{shutdownHostWorkLossMode, shutdownHostWorkCancelHandlerMode}
					if invocation == shutdownToolInvocation {
						modes = []string{shutdownHostWorkMode, shutdownHostWorkLossMode, shutdownHostWorkCancelMode}
					}
					for _, mode := range modes {
						t.Run(mode, func(t *testing.T) {
							t.Parallel()
							// Act and assert failure settlement through the production runtime and manager.
							assertManagerFailureShutdown(t, mode, invocation, monitored, shutdownCloseCompletion)
						})
					}
				})
			}
		})
	}
}

// TestManagerRemovalJoinsFailedRuntimeGeneration checks replacement and rejection of a failed process owner.
func TestManagerRemovalJoinsFailedRuntimeGeneration(t *testing.T) {
	t.Parallel()
	// Arrange removal paths for an observed failed runtime.
	for _, completion := range []string{shutdownReplacementCompletion, shutdownRejectionCompletion} {
		t.Run(completion, func(t *testing.T) {
			t.Parallel()
			// Act and assert owner cleanup for the selected removal path.
			assertManagerFailureShutdown(t, shutdownHostWorkMode, shutdownToolInvocation, true, completion)
		})
	}
}

// writeRuntimeExecutable prepares a fixture executable without exposing its writable descriptor to concurrent forks.
func writeRuntimeExecutable(path, script string) error {
	// A fork can temporarily inherit even a close-on-exec descriptor and prevent execution of that file.
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	return os.WriteFile(path, []byte(script), 0o755)
}
