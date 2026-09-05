package nodes

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NodeIdentity is a persistent Ed25519 keypair. NodeID is hex(pubkey) so votes
// cannot spoof another node's id without its private key.
type NodeIdentity struct {
	NodeID  string `json:"nodeId"`
	PubKey  string `json:"pubKey"`
	PrivKey string `json:"privKey"` // hex-encoded ed25519.PrivateKey
}

type identityFile struct {
	NodeID  string `json:"nodeId"`
	PubKey  string `json:"pubKey"`
	PrivKey string `json:"privKey"`
}

// DefaultIdentityPath resolves PLATARIUM_NODE_IDENTITY_FILE or <dataDir>/node_identity.json.
func DefaultIdentityPath() string {
	return IdentityPathForPort(0)
}

// IdentityPathForPort returns a per-process identity path. When port > 0 and
// PLATARIUM_NODE_IDENTITY_FILE is unset, the filename includes the WS port so
// multi-node local scripts do not share one keypair.
func IdentityPathForPort(port int) string {
	if p := strings.TrimSpace(os.Getenv("PLATARIUM_NODE_IDENTITY_FILE")); p != "" {
		return p
	}
	dataDir := strings.TrimSpace(os.Getenv("PLATARIUM_DATA_DIR"))
	if dataDir == "" {
		if sf := strings.TrimSpace(os.Getenv("PLATARIUM_STATE_FILE")); sf != "" {
			dataDir = filepath.Dir(sf)
		} else {
			dataDir = "data"
		}
	}
	if port > 0 {
		return filepath.Join(dataDir, fmt.Sprintf("node_identity_%d.json", port))
	}
	return filepath.Join(dataDir, "node_identity.json")
}

// LoadOrCreateIdentity loads a persisted identity or creates a new one.
func LoadOrCreateIdentity(path string) (*NodeIdentity, error) {
	if path == "" {
		path = DefaultIdentityPath()
	}
	if b, err := os.ReadFile(path); err == nil {
		var f identityFile
		if err := json.Unmarshal(b, &f); err != nil {
			return nil, fmt.Errorf("parse node identity: %w", err)
		}
		if f.PrivKey == "" || f.PubKey == "" {
			return nil, fmt.Errorf("incomplete node identity in %s", path)
		}
		priv, err := hex.DecodeString(f.PrivKey)
		if err != nil || len(priv) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("invalid privKey in %s", path)
		}
		pub, err := hex.DecodeString(f.PubKey)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid pubKey in %s", path)
		}
		nodeID := f.NodeID
		if nodeID == "" {
			nodeID = hex.EncodeToString(pub)
		}
		expected := hex.EncodeToString(pub)
		if nodeID != expected {
			return nil, fmt.Errorf("nodeId does not match pubKey in %s", path)
		}
		return &NodeIdentity{NodeID: nodeID, PubKey: f.PubKey, PrivKey: f.PrivKey}, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	id := &NodeIdentity{
		NodeID:  hex.EncodeToString(pub),
		PubKey:  hex.EncodeToString(pub),
		PrivKey: hex.EncodeToString(priv),
	}
	if err := id.Save(path); err != nil {
		return nil, err
	}
	return id, nil
}

// Save writes the identity file (0600).
func (id *NodeIdentity) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(identityFile{
		NodeID:  id.NodeID,
		PubKey:  id.PubKey,
		PrivKey: id.PrivKey,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// VoteMessage is the canonical signed payload for L1/L2 peer votes.
func VoteMessage(blockID string, yes bool) []byte {
	y := "0"
	if yes {
		y = "1"
	}
	return []byte("platarium-vote-v1|" + blockID + "|" + y)
}

// SignVote returns hex(ed25519 signature) over VoteMessage.
func (id *NodeIdentity) SignVote(blockID string, yes bool) (string, error) {
	priv, err := hex.DecodeString(id.PrivKey)
	if err != nil || len(priv) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("bad private key")
	}
	sig := ed25519.Sign(ed25519.PrivateKey(priv), VoteMessage(blockID, yes))
	return hex.EncodeToString(sig), nil
}

// VerifyVoteSignature checks that pubKeyHex signs the vote and matches nodeID.
func VerifyVoteSignature(nodeID, blockID string, yes bool, pubKeyHex, sigHex string) error {
	pub, err := hex.DecodeString(pubKeyHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid vote pubKey")
	}
	if hex.EncodeToString(pub) != nodeID {
		return fmt.Errorf("vote nodeId/pubKey mismatch")
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("invalid vote signature encoding")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), VoteMessage(blockID, yes), sig) {
		return fmt.Errorf("vote signature verify failed")
	}
	return nil
}

// RequireSignedVotes is true unless PLATARIUM_ALLOW_UNSIGNED_VOTES enables legacy peers.
func RequireSignedVotes() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("PLATARIUM_ALLOW_UNSIGNED_VOTES")))
	switch v {
	case "1", "true", "yes", "on":
		return false
	default:
		return true
	}
}
