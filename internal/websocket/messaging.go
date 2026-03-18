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
	// Take buffered offline messages (if any) for this address
	pending := s.offlineMessages[address]
	delete(s.offlineMessages, address)
	s.mu.Unlock()

	log.Printf("[MESSAGE] Client %s registered address: %s", client.ID[:8], address)

	// Send confirmation
	client.Conn.WriteJSON(map[string]interface{}{
		"type": "registered",
		"data": map[string]interface{}{
			"address": address,
		},
	})

	// Deliver any buffered messages to this client
	if len(pending) > 0 {
		log.Printf("[MESSAGE] Delivering %d buffered message(s) to %s", len(pending), address)
		for _, m := range pending {
			msg := map[string]interface{}{
				"type": "message",
				"data": map[string]interface{}{
					"from":      m.From,
					"to":        m.To,
					"text":      m.Text,
					"timestamp": m.Timestamp,
				},
			}
			client.mu.Lock()
			if err := client.Conn.WriteJSON(msg); err != nil {
				log.Printf("[MESSAGE] Error delivering buffered message to %s: %v", address, err)
				client.mu.Unlock()
				break
			}
			client.mu.Unlock()
		}
	}
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
		// Recipient is offline on this node: buffer message for later delivery
		log.Printf("[MESSAGE] Recipient %s offline, buffering message", to)
		s.mu.Lock()
		buf := s.offlineMessages[to]
		now := time.Now().Unix()
		buf = append(buf, OfflineMessage{
			From:      from,
			To:        to,
			Text:      text,
			Timestamp: now,
		})
		// Optional: limit buffer size per recipient to avoid unbounded growth
		if len(buf) > 100 {
			buf = buf[len(buf)-100:]
		}
		s.offlineMessages[to] = buf
		s.mu.Unlock()

		// Try to find recipient on peer nodes as well (for multi-node setups)
		if s.nodesManager != nil {
			s.routeMessageToPeer(to, from, text)
		}

		// Acknowledge to sender that message is accepted for delivery (buffered)
		sender.Conn.WriteJSON(map[string]interface{}{
			"type": "messageSent",
			"data": map[string]interface{}{
				"to":        to,
				"timestamp": time.Now().Unix(),
			},
		})
		return
	}

	// Recipient is online locally: send message directly
	now := time.Now().Unix()
	message := map[string]interface{}{
		"type": "message",
		"data": map[string]interface{}{
			"from":      from,
			"to":        to,
			"text":      text,
			"timestamp": now,
		},
	}

	recipient.mu.Lock()
	err := recipient.Conn.WriteJSON(message)
	recipient.mu.Unlock()

	if err != nil {
		log.Printf("[MESSAGE] Error sending message to %s (connection dead): %v; buffering for later", to, err)
		s.mu.Lock()
		delete(s.clientsByAddr, to)
		buf := s.offlineMessages[to]
		buf = append(buf, OfflineMessage{From: from, To: to, Text: text, Timestamp: now})
		if len(buf) > 100 {
			buf = buf[len(buf)-100:]
		}
		s.offlineMessages[to] = buf
		s.mu.Unlock()
		sender.Conn.WriteJSON(map[string]interface{}{
			"type": "messageSent",
			"data": map[string]interface{}{
				"to":        to,
				"timestamp": now,
			},
		})
		return
	}

	log.Printf("[MESSAGE] Message delivered from %s to %s", from, to)

	// Send delivery confirmation to sender
	sender.Conn.WriteJSON(map[string]interface{}{
		"type": "messageSent",
		"data": map[string]interface{}{
			"to":        to,
			"timestamp": now,
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

// HandleIncomingPeerMessage handles messages from peer nodes (called for eventType "message:route";
// data is the event payload: from, to, text, timestamp - no "type" field).
func (s *Server) HandleIncomingPeerMessage(data map[string]interface{}) {
	to, _ := data["to"].(string)
	from, _ := data["from"].(string)
	text, _ := data["text"].(string)
	if to == "" || from == "" || text == "" {
		return
	}

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
	} else {
		// Recipient offline on this node as well: buffer for later when they come online here
		log.Printf("[MESSAGE] Recipient %s offline on this node (peer route), buffering message", to)
		s.mu.Lock()
		ts, _ := data["timestamp"].(int64)
		if ts == 0 {
			ts = time.Now().Unix()
		}
		buf := s.offlineMessages[to]
		buf = append(buf, OfflineMessage{
			From:      from,
			To:        to,
			Text:      text,
			Timestamp: ts,
		})
		if len(buf) > 100 {
			buf = buf[len(buf)-100:]
		}
		s.offlineMessages[to] = buf
		s.mu.Unlock()
	}
}






















