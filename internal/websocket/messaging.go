package websocket

import (
	"log"
	"time"
)

// handleClientRegister handles client address registration
func (s *Server) handleClientRegister(client *Client, data map[string]interface{}) {
	address, ok := data["address"].(string)
	if !ok || address == "" {
		log.Printf("[MESSAGE] Invalid address registration from client %s", client.ID)
		return
	}

	s.mu.Lock()
	// Remove old address mapping if exists
	if client.Address != "" && client.Address != address {
		delete(s.clientsByAddr, client.Address)
	}
	
	// Update client address
	client.Address = address
	s.clientsByAddr[address] = client
	s.mu.Unlock()

	log.Printf("[MESSAGE] Client %s registered address: %s", client.ID[:8], address)

	// Send confirmation
	client.Conn.WriteJSON(map[string]interface{}{
		"type": "registered",
		"data": map[string]interface{}{
			"address": address,
		},
	})
}

// handleDirectMessage routes a message to the recipient by address
func (s *Server) handleDirectMessage(sender *Client, data map[string]interface{}) {
	to, _ := data["to"].(string)
	text, _ := data["text"].(string)
	from := sender.Address

	if to == "" || text == "" {
		log.Printf("[MESSAGE] Invalid message format from client %s", sender.ID)
		sender.Conn.WriteJSON(map[string]interface{}{
			"type": "messageError",
			"data": map[string]interface{}{
				"error": "Invalid message format: 'to' and 'text' are required",
			},
		})
		return
	}

	if from == "" {
		log.Printf("[MESSAGE] Sender %s not registered", sender.ID)
		sender.Conn.WriteJSON(map[string]interface{}{
			"type": "messageError",
			"data": map[string]interface{}{
				"error": "You must register your address first",
			},
		})
		return
	}

	// Find recipient by address
	s.mu.RLock()
	recipient, found := s.clientsByAddr[to]
	s.mu.RUnlock()

	if !found {
		// Try to find recipient on peer nodes
		if s.nodesManager != nil {
			s.routeMessageToPeer(to, from, text)
		} else {
			log.Printf("[MESSAGE] Recipient %s not found", to)
			sender.Conn.WriteJSON(map[string]interface{}{
				"type": "messageError",
				"data": map[string]interface{}{
					"error": "Recipient not found",
					"to":    to,
				},
			})
		}
		return
	}

	// Send message to recipient
	message := map[string]interface{}{
		"type": "message",
		"data": map[string]interface{}{
			"from":      from,
			"to":        to,
			"text":      text,
			"timestamp": time.Now().Unix(),
		},
	}

	recipient.mu.Lock()
	if err := recipient.Conn.WriteJSON(message); err != nil {
		log.Printf("[MESSAGE] Error sending message to %s: %v", to, err)
		recipient.mu.Unlock()
		sender.Conn.WriteJSON(map[string]interface{}{
			"type": "messageError",
			"data": map[string]interface{}{
				"error": "Failed to deliver message",
			},
		})
		return
	}
	recipient.mu.Unlock()

	log.Printf("[MESSAGE] Message delivered from %s to %s", from, to)

	// Send delivery confirmation to sender
	sender.Conn.WriteJSON(map[string]interface{}{
		"type": "messageSent",
		"data": map[string]interface{}{
			"to":        to,
			"timestamp": time.Now().Unix(),
		},
	})
}

// routeMessageToPeer routes message to peer nodes if recipient is not local
func (s *Server) routeMessageToPeer(to, from, text string) {
	// Broadcast message to all peer nodes
	// They will check if they have the recipient and forward if found
	messageData := map[string]interface{}{
		"type": "message:route",
		"data": map[string]interface{}{
			"from":      from,
			"to":        to,
			"text":      text,
			"timestamp": time.Now().Unix(),
		},
	}

	if s.nodesManager != nil {
		s.nodesManager.BroadcastBlockchainEvent("message:route", messageData["data"].(map[string]interface{}), "")
	}
}

// HandleIncomingPeerMessage handles messages from peer nodes
func (s *Server) HandleIncomingPeerMessage(data map[string]interface{}) {
	msgType, _ := data["type"].(string)
	
	if msgType == "message:route" {
		to, _ := data["to"].(string)
		from, _ := data["from"].(string)
		text, _ := data["text"].(string)

		// Check if recipient is local
		s.mu.RLock()
		recipient, found := s.clientsByAddr[to]
		s.mu.RUnlock()

		if found {
			// Deliver message to local recipient
			message := map[string]interface{}{
				"type": "message",
				"data": map[string]interface{}{
					"from":      from,
					"to":        to,
					"text":      text,
					"timestamp": data["timestamp"],
				},
			}

			recipient.mu.Lock()
			recipient.Conn.WriteJSON(message)
			recipient.mu.Unlock()

			log.Printf("[MESSAGE] Message routed from peer: %s -> %s", from, to)
		}
	}
}

