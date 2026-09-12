package websocket

import (
	"fmt"
	"testing"
)

func TestReconnectWithoutProofUnauthenticated(t *testing.T) {
	s := &Server{
		clients:       make(map[string]*Client),
		clientsByAddr: make(map[string]map[string]*Client),
		offlineMessages: make(map[string][]OfflineMessage),
		e2eePubKeys:   make(map[string]string),
	}
	s.SetOwnershipProver(func(address, signature, mnemonic, alphanumeric, pubMain string) error {
		if mnemonic == "good" && alphanumeric == "a" {
			return nil
		}
		return fmt.Errorf("bad proof")
	})

	c1 := &Client{ID: "sess-1"}
	s.clients[c1.ID] = c1
	payload, err := s.bindClientRegister(c1, map[string]interface{}{
		"address": "PxOwner", "deviceId": "dev-1", "mnemonic": "good", "alphanumeric": "a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload["authenticated"] != true || !c1.Authenticated {
		t.Fatal("initial register with proof must authenticate")
	}

	// Simulate disconnect cleanup then reconnect without proof (new socket).
	s.mu.Lock()
	delete(s.clients, c1.ID)
	s.removeClientFromAddrLocked(c1.Address, c1.ID)
	s.mu.Unlock()

	c2 := &Client{ID: "sess-2"}
	s.clients[c2.ID] = c2
	payload, err = s.bindClientRegister(c2, map[string]interface{}{
		"address": "PxOwner", "deviceId": "dev-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload["authenticated"] != false || c2.Authenticated {
		t.Fatal("reconnect without proof must be unauthenticated")
	}
	if c2.AuthenticatedOwner() != "" {
		t.Fatal("AuthenticatedOwner must be empty after reconnect without proof")
	}

	// Restore clean state for package tests.
	s.mu.Lock()
	s.clients = make(map[string]*Client)
	s.clientsByAddr = make(map[string]map[string]*Client)
	s.mu.Unlock()
}

func TestDuplicateRegisterCannotHijackAuthenticatedSession(t *testing.T) {
	s := &Server{
		clients:         make(map[string]*Client),
		clientsByAddr:   make(map[string]map[string]*Client),
		offlineMessages: make(map[string][]OfflineMessage),
		e2eePubKeys:     make(map[string]string),
	}
	s.SetOwnershipProver(func(address, signature, mnemonic, alphanumeric, pubMain string) error {
		if mnemonic == "victim" {
			return nil
		}
		return fmt.Errorf("bad proof")
	})

	victim := &Client{ID: "victim-sess"}
	s.clients[victim.ID] = victim
	if _, err := s.bindClientRegister(victim, map[string]interface{}{
		"address": "PxVictim", "deviceId": "phone", "mnemonic": "victim", "alphanumeric": "a",
	}); err != nil {
		t.Fatal(err)
	}
	if !victim.Authenticated {
		t.Fatal("victim must be authenticated")
	}

	// Attacker reuses same deviceId without proof — replaces socket but must not inherit auth.
	attacker := &Client{ID: "attacker-sess"}
	s.clients[attacker.ID] = attacker
	payload, err := s.bindClientRegister(attacker, map[string]interface{}{
		"address": "PxVictim", "deviceId": "phone",
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload["authenticated"] != false || attacker.Authenticated {
		t.Fatal("duplicate register without proof must not authenticate attacker")
	}
	if victim.Authenticated {
		t.Fatal("replaced victim session must clear Authenticated (no hijack)")
	}
	if victim.Address != "" {
		t.Fatal("replaced victim session must clear Address")
	}
	if attacker.AuthenticatedOwner() != "" {
		t.Fatal("attacker AuthenticatedOwner must be empty")
	}

	// Clean restore.
	s.mu.Lock()
	s.clients = make(map[string]*Client)
	s.clientsByAddr = make(map[string]map[string]*Client)
	s.mu.Unlock()
}
