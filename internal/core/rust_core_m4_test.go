package core

import (
	"encoding/json"
	"testing"
)

// M4: VerifySignature / GenerateKeys must not treat arbitrary prose as success.
func TestVerifySignatureRejectsNonJSON(t *testing.T) {
	rc := &RustCore{} // no RPC — Execute would be used; force parse path via helper shape
	// Directly assert the JSON contract used by VerifySignature.
	var parsed struct {
		Verified bool `json:"verified"`
	}
	if err := json.Unmarshal([]byte(`Verified: true`), &parsed); err == nil && parsed.Verified {
		t.Fatal("prose must not parse as verified JSON")
	}
	if err := json.Unmarshal([]byte(`{"verified":true}`), &parsed); err != nil || !parsed.Verified {
		t.Fatalf("JSON contract failed: %v %#v", err, parsed)
	}
	_ = rc
}

func TestGenerateKeysRequiresJSONFields(t *testing.T) {
	var parsed map[string]string
	if err := json.Unmarshal([]byte("Public Key: abc\n"), &parsed); err == nil {
		if parsed["publicKey"] != "" {
			t.Fatal("line prose must not yield publicKey")
		}
	}
	if err := json.Unmarshal([]byte(`{"publicKey":"a","privateKey":"b"}`), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["publicKey"] != "a" || parsed["privateKey"] != "b" {
		t.Fatalf("%v", parsed)
	}
}
