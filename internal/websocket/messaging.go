package websocket

import (
	"encoding/json"
	"log"
	"time"

	"platarium-gateway-go/internal/contacteconomy"
	"platarium-gateway-go/internal/protocol"
)

// handleClientRegister handles client address registration
func (s *Server) handleClientRegister(client *Client, data map[string]interface{}) {
	addressRaw, ok := data["address"].(string)
	if !ok || addressRaw == "" {
		log.Printf("[MESSAGE] Invalid address registration from client %s", client.ID)
		return
	}
	address := normalizePlatariumAddress(addressRaw)

	var announceAddr, announcePk string

	s.mu.Lock()
	// Another tab may still hold this address; drop stale mapping so only this socket receives.
	if existing, ok := s.clientsByAddr[address]; ok && existing != nil && existing.ID != client.ID {
		existing.Address = ""
	}
	// Remove old address mapping if exists
	if client.Address != "" {
		old := normalizePlatariumAddress(client.Address)
		if old != address {
			delete(s.clientsByAddr, old)
			delete(s.clientsByAddr, client.Address)
		}
	}

	// Update client address
	client.Address = address
	s.clientsByAddr[address] = client
	if pk, ok := data["e2eePublicKey"].(string); ok && pk != "" {
		s.e2eePubKeys[address] = pk
		announceAddr, announcePk = address, pk
	}
	// Take buffered offline messages (if any) for this address
	pending := s.offlineMessages[address]
	if len(pending) == 0 && addressRaw != address {
		pending = s.offlineMessages[addressRaw]
		delete(s.offlineMessages, addressRaw)
	}
	delete(s.offlineMessages, address)
	s.mu.Unlock()

	now := time.Now().Unix()
	filtered := pending[:0]
	for _, m := range pending {
		if offlineMessageAgeOK(m, now) {
			filtered = append(filtered, m)
		}
	}
	pending = filtered

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

	if announcePk != "" {
		go s.broadcastE2eePubKeyAnnouncement(announceAddr, announcePk)
	}
}

// handleDirectMessage routes a message to the recipient by address
func (s *Server) handleDirectMessage(sender *Client, data map[string]interface{}) {
	toRaw, _ := data["to"].(string)
	text, _ := data["text"].(string)
	from := sender.Address

	if toRaw == "" || text == "" {
		log.Printf("[MESSAGE] Invalid message format from client %s", sender.ID)
		sender.Conn.WriteJSON(map[string]interface{}{
			"type": "messageError",
			"data": map[string]interface{}{
				"error": "Invalid message format: 'to' and 'text' are required",
			},
		})
		return
	}
	if len(text) > maxDirectMessageTextBytes {
		log.Printf("[MESSAGE] Rejected oversized message from %s (%d bytes)", sender.ID, len(text))
		sender.Conn.WriteJSON(map[string]interface{}{
			"type": "messageError",
			"data": map[string]interface{}{
				"error": "Message too large",
			},
		})
		return
	}

	to := normalizePlatariumAddress(toRaw)

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

	s.mu.RLock()
	ce := s.contactEconomy
	s.mu.RUnlock()
	if ce != nil && !ce.CanSendFreeDM(from, to) {
		log.Printf("[MESSAGE] First-contact gate: %s -> %s requires contact request + PLP lock", from, to)
		sender.Conn.WriteJSON(map[string]interface{}{
			"type": "messageError",
			"data": map[string]interface{}{
				"error": "protocol_contact_required",
				"to":    to,
			},
		})
		return
	}

	// Find recipient by address (normalized + raw fallback for legacy clients)
	s.mu.RLock()
	recipient, found := s.clientsByAddr[to]
	if !found && toRaw != to {
		recipient, found = s.clientsByAddr[toRaw]
	}
	s.mu.RUnlock()

	writeMessageSent := func(delivered bool) {
		sender.Conn.WriteJSON(map[string]interface{}{
			"type": "messageSent",
			"data": map[string]interface{}{
				"to":        to,
				"timestamp": time.Now().Unix(),
				"delivered": delivered,
			},
		})
	}

	if !found {
		// Recipient is offline on this node: buffer message for later delivery (up to 24h, see server.go TTL)
		log.Printf("[MESSAGE] Recipient %s offline on node, buffering (from %s)", to, from)
		s.mu.Lock()
		buf := s.offlineMessages[to]
		now := time.Now().Unix()
		buf = append(buf, OfflineMessage{
			From:       from,
			To:         to,
			Text:       text,
			Timestamp:  now,
			BufferedAt: now,
		})
		if len(buf) > offlineMessageMaxPerRecipient {
			buf = buf[len(buf)-offlineMessageMaxPerRecipient:]
		}
		s.offlineMessages[to] = buf
		s.mu.Unlock()

		// Try to find recipient on peer nodes as well (for multi-node setups)
		if s.nodesManager != nil {
			s.routeMessageToPeer(to, from, text)
		}

		// Acknowledge to sender that message is accepted for delivery (buffered)
		writeMessageSent(false)
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
		buf = append(buf, OfflineMessage{
			From:       from,
			To:         to,
			Text:       text,
			Timestamp:  now,
			BufferedAt: time.Now().Unix(),
		})
		if len(buf) > offlineMessageMaxPerRecipient {
			buf = buf[len(buf)-offlineMessageMaxPerRecipient:]
		}
		s.offlineMessages[to] = buf
		s.mu.Unlock()
		writeMessageSent(false)
		return
	}

	log.Printf("[MESSAGE] Message delivered from %s to %s", from, to)

	// Send delivery confirmation to sender
	writeMessageSent(true)
}

// handleE2eePubKeyRequest returns a peer's last registered X25519 public key (base64) for E2EE.
func (s *Server) handleE2eePubKeyRequest(client *Client, data map[string]interface{}) {
	ofAddressRaw, _ := data["address"].(string)
	requestID, _ := data["requestId"].(string)
	if ofAddressRaw == "" || requestID == "" {
		return
	}
	ofAddress := normalizePlatariumAddress(ofAddressRaw)
	s.mu.RLock()
	pk := s.e2eePubKeys[ofAddress]
	if pk == "" && ofAddressRaw != ofAddress {
		pk = s.e2eePubKeys[ofAddressRaw]
	}
	s.mu.RUnlock()
	payload := map[string]interface{}{
		"address":   ofAddress,
		"requestId": requestID,
	}
	if pk != "" {
		payload["publicKey"] = pk
	}
	_ = client.Conn.WriteJSON(map[string]interface{}{
		"type": "e2eePubKey",
		"data": payload,
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
	toRaw, _ := data["to"].(string)
	fromRaw, _ := data["from"].(string)
	text, _ := data["text"].(string)
	if toRaw == "" || fromRaw == "" || text == "" {
		return
	}
	to := normalizePlatariumAddress(toRaw)
	from := normalizePlatariumAddress(fromRaw)

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
		ts := int64(0)
		switch v := data["timestamp"].(type) {
		case int64:
			ts = v
		case float64:
			ts = int64(v)
		case int:
			ts = int64(v)
		}
		if ts == 0 {
			ts = time.Now().Unix()
		}
		queuedAt := time.Now().Unix()
		buf := s.offlineMessages[to]
		buf = append(buf, OfflineMessage{
			From:       from,
			To:         to,
			Text:       text,
			Timestamp:  ts,
			BufferedAt: queuedAt,
		})
		if len(buf) > offlineMessageMaxPerRecipient {
			buf = buf[len(buf)-offlineMessageMaxPerRecipient:]
		}
		s.offlineMessages[to] = buf
		s.mu.Unlock()
	}
}

// LookupE2eePubKey returns the last registered X25519 public key for an address (if any).
func (s *Server) LookupE2eePubKey(address string) string {
	norm := normalizePlatariumAddress(address)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if pk := s.e2eePubKeys[norm]; pk != "" {
		return pk
	}
	return s.e2eePubKeys[address]
}

// broadcastE2eePubKeyAnnouncement notifies all connected messenger clients so senders can encrypt
// without polling when a recipient registers their key on this gateway.
func (s *Server) broadcastE2eePubKeyAnnouncement(addr, publicKey string) {
	if publicKey == "" || addr == "" {
		return
	}
	payload := map[string]interface{}{
		"type": "e2eePubKey",
		"data": map[string]interface{}{
			"address":   addr,
			"publicKey": publicKey,
		},
	}
	s.mu.RLock()
	snapshot := make([]*Client, 0, len(s.clients))
	for _, c := range s.clients {
		snapshot = append(snapshot, c)
	}
	s.mu.RUnlock()
	for _, c := range snapshot {
		if c == nil {
			continue
		}
		c.mu.Lock()
		_ = c.Conn.WriteJSON(payload)
		c.mu.Unlock()
	}
}























func (s *Server) handleProtocolContactQuery(client *Client, data map[string]interface{}) {
	peerRaw, _ := data["peer"].(string)
	requestID, _ := data["requestId"].(string)
	from := client.Address
	s.mu.RLock()
	ce := s.contactEconomy
	s.mu.RUnlock()
	established := true
	if ce != nil && ce.Config().Enabled {
		established = ce.HasProtocolContact(from, peerRaw)
	}
	_ = client.Conn.WriteJSON(map[string]interface{}{
		"type": "protocolContactResult",
		"data": map[string]interface{}{
			"peer":        normalizePlatariumAddress(peerRaw),
			"established": established,
			"requestId":   requestID,
		},
	})
}

func (s *Server) handleContactRequestWS(client *Client, data map[string]interface{}) {
	s.mu.RLock()
	ce := s.contactEconomy
	s.mu.RUnlock()
	if ce == nil {
		_ = client.Conn.WriteJSON(map[string]interface{}{
			"type": "contactRequestError",
			"data": map[string]interface{}{"error": "contact economy unavailable"},
		})
		return
	}
	req := contacteconomy.ContactRequest{
		RequestID:        strField(data, "requestId"),
		Sender:           client.Address,
		Receiver:         strField(data, "receiver"),
		SenderPubKey:     strField(data, "senderPublicKey"),
		ReceiverPubKey:   strField(data, "receiverPublicKey"),
		EncryptedPayload: strField(data, "encryptedPayload"),
		LockTxHash:       strField(data, "lockTxHash"),
	}
	if amt, ok := data["amountUplp"].(float64); ok {
		req.AmountUplp = uint64(amt)
	}
	created, err := ce.CreateRequest(req)
	if err != nil {
		_ = client.Conn.WriteJSON(map[string]interface{}{
			"type": "contactRequestError",
			"data": map[string]interface{}{"error": err.Error()},
		})
		return
	}
	s.DeliverContactRequest(created)
	_ = client.Conn.WriteJSON(map[string]interface{}{
		"type": "contactRequestAck",
		"data": map[string]interface{}{"request": created},
	})
}

func (s *Server) handleContactRespondWS(client *Client, data map[string]interface{}) {
	s.mu.RLock()
	ce := s.contactEconomy
	s.mu.RUnlock()
	if ce == nil {
		return
	}
	requestID := strField(data, "requestId")
	outcome := strField(data, "outcome")
	sig := strField(data, "signature")
	enc := strField(data, "encryptedResponse")
	req, err := ce.Respond(requestID, client.Address, outcome, sig)
	if err != nil {
		_ = client.Conn.WriteJSON(map[string]interface{}{
			"type": "contactRespondError",
			"data": map[string]interface{}{"error": err.Error()},
		})
		return
	}
	if outcome == contacteconomy.OutcomeAccepted {
		ce.AddXP(client.Address, 25)
		ce.AddXP(req.Sender, 10)
	}
	s.NotifyContactResolved(req, enc)
	intent := protocol.ContactSettleFromRequest(req, "")
	_ = client.Conn.WriteJSON(map[string]interface{}{
		"type": "contactRespondAck",
		"data": map[string]interface{}{
			"request":      req,
			"settleIntent": intent,
		},
	})
}

func (s *Server) handleContactPricingAnnounce(client *Client, data map[string]interface{}) {
	s.mu.RLock()
	ce := s.contactEconomy
	s.mu.RUnlock()
	if ce == nil {
		return
	}
	p := contacteconomy.PricingAnnounce{
		Address:   client.Address,
		Signature: strField(data, "signature"),
		Blocked:   false,
	}
	if v, ok := data["blocked"].(bool); ok {
		p.Blocked = v
	}
	if v, ok := data["unknownFeeUplp"].(float64); ok {
		p.UnknownFeeUplp = uint64(v)
	}
	if v, ok := data["verifiedFeeUplp"].(float64); ok {
		p.VerifiedFeeUplp = uint64(v)
	}
	out, err := ce.SetPricing(p)
	if err != nil {
		_ = client.Conn.WriteJSON(map[string]interface{}{
			"type": "contactPricingError",
			"data": map[string]interface{}{"error": err.Error()},
		})
		return
	}
	_ = client.Conn.WriteJSON(map[string]interface{}{
		"type": "contactPricingAck",
		"data": map[string]interface{}{"pricing": out},
	})
}

func strField(data map[string]interface{}, key string) string {
	v, _ := data[key].(string)
	return v
}

// DeliverContactRequest pushes ciphertext request to the receiver (online or offline buffer).
func (s *Server) DeliverContactRequest(req contacteconomy.ContactRequest) {
	payload := map[string]interface{}{
		"type": "contactRequest",
		"data": map[string]interface{}{
			"requestId":         req.RequestID,
			"sender":            req.Sender,
			"receiver":          req.Receiver,
			"senderPublicKey":   req.SenderPubKey,
			"receiverPublicKey": req.ReceiverPubKey,
			"encryptedPayload":  req.EncryptedPayload,
			"timestamp":         req.Timestamp,
			"expiresAt":         req.ExpiresAt,
			"amountUplp":        req.AmountUplp,
			"lockTxHash":        req.LockTxHash,
		},
	}
	to := normalizePlatariumAddress(req.Receiver)
	s.mu.RLock()
	recipient, found := s.clientsByAddr[to]
	s.mu.RUnlock()
	if found && recipient != nil {
		recipient.mu.Lock()
		_ = recipient.Conn.WriteJSON(payload)
		recipient.mu.Unlock()
		return
	}
	// Buffer as offline message with special prefix so client can distinguish.
	text, _ := json.Marshal(payload)
	s.mu.Lock()
	now := time.Now().Unix()
	buf := s.offlineMessages[to]
	buf = append(buf, OfflineMessage{
		From:       req.Sender,
		To:         to,
		Text:       string(text),
		Timestamp:  now,
		BufferedAt: now,
	})
	s.offlineMessages[to] = buf
	s.mu.Unlock()
}

// NotifyContactResolved notifies both parties of accept/reject.
func (s *Server) NotifyContactResolved(req contacteconomy.ContactRequest, encryptedResponse string) {
	payload := map[string]interface{}{
		"type": "contactResolved",
		"data": map[string]interface{}{
			"requestId":          req.RequestID,
			"status":             req.Status,
			"settleOutcome":      req.SettleOutcome,
			"sender":             req.Sender,
			"receiver":           req.Receiver,
			"encryptedResponse":  encryptedResponse,
		},
	}
	for _, addr := range []string{req.Sender, req.Receiver} {
		a := normalizePlatariumAddress(addr)
		s.mu.RLock()
		c := s.clientsByAddr[a]
		s.mu.RUnlock()
		if c == nil {
			continue
		}
		c.mu.Lock()
		_ = c.Conn.WriteJSON(payload)
		c.mu.Unlock()
	}
}
