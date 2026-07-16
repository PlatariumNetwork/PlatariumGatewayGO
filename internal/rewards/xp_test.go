package rewards

import "testing"

func TestComputeValidationXPMinFloor(t *testing.T) {
	xp := ComputeValidationXP(0, 1, 10, 10)
	if xp < MinValidationXP {
		t.Fatalf("xp=%d want >= %d", xp, MinValidationXP)
	}
}

func TestComputeValidationXPScalesWithLoad(t *testing.T) {
	low := ComputeValidationXP(0, 100, 1000, 10)
	high := ComputeValidationXP(90, 100, 1000, 10)
	if high <= low {
		t.Fatalf("high-load xp=%d should exceed low-load xp=%d", high, low)
	}
}

func TestComputeValidationXPScalesWithWeight(t *testing.T) {
	equal := ComputeValidationXP(50, 100, 1000, 10)
	heavy := ComputeValidationXP(50, 300, 1000, 10)
	if heavy <= equal {
		t.Fatalf("heavy-weight xp=%d should exceed equal xp=%d", heavy, equal)
	}
}

func TestOperatorWalletFromEnvNormalization(t *testing.T) {
	t.Setenv("PLATARIUM_OPERATOR_WALLET", "pxABCDEF")
	got := OperatorWalletFromEnv()
	if got != "Pxabcdef" {
		t.Fatalf("got %q want Pxabcdef", got)
	}
}
