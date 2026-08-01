package core

import (
	"os"
	"testing"
)

func TestKernelExecEnabledDefaultOn(t *testing.T) {
	os.Unsetenv("PLATARIUM_KERNEL_EXEC")
	if !KernelExecEnabled() {
		t.Fatal("expected kernel exec on by default")
	}
	os.Setenv("PLATARIUM_KERNEL_EXEC", "0")
	defer os.Unsetenv("PLATARIUM_KERNEL_EXEC")
	if KernelExecEnabled() {
		t.Fatal("expected off when PLATARIUM_KERNEL_EXEC=0")
	}
}
