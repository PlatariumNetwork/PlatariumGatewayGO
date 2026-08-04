package websocket

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"platarium-gateway-go/internal/contacteconomy"
	"platarium-gateway-go/internal/protocol"
)

// --- multi-device helpers (callers must hold s.mu when using *Locked variants) ---

func (s *Server) removeClientFromAddrLocked(address, clientID string) {
	m := s.clientsByAddr[address]
	if m == nil {
		return
	}
	delete(m, clientID)
	if len(m) == 0 {
		delete(s.clientsByAddr, address)
	}
}

func (s *Server) addClientToAddrLocked(address string, client *Client) {
	m := s.clientsByAddr[address]
	if m == nil {
		m = make(map[string]*Client)
		s.clientsByAddr[address] = m
	}
	// Same deviceId reconnecting: drop previous socket for that device.
	if client.DeviceID != "" {
		for id, c := range m {
			if c != nil && c.DeviceID == client.DeviceID && c.ID != client.ID {
				c.Address = ""
				delete(m, id)
				go func(old *Client) {
					old.mu.Lock()
					_ = old.Conn.WriteJSON(map[string]interface{}{
						"type": "forceLogout",
						"data": map[string]interface{}{
							"reason": "device_replaced",
						},
					})
					_ = old.Conn.Close()
					old.mu.Unlock()
				}(c)
			}
		}
	}
	m[client.ID] = client
}

func (s *Server) snapshotClientsForAddr(address string) []*Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotClientsForAddrLocked(address)
}

func (s *Server) snapshotClientsForAddrLocked(address string) []*Client {
	m := s.clientsByAddr[address]
	if len(m) == 0 {
		return nil
	}
	out := make([]*Client, 0, len(m))
	for _, c := range m {
		if c != nil {
			out = append(out, c)
		}
	}
	return out
}

func (s *Server) deviceEntriesForAddrLocked(address string) []map[string]interface{} {
	m := s.clientsByAddr[address]
	out := make([]map[string]interface{}, 0, len(m))
	for _, c := range m {
		if c == nil {
			continue
		}
		out = append(out, map[string]interface{}{
			"deviceId":    c.DeviceID,
			"deviceLabel": c.DeviceLabel,
			"clientId":    c.ID,
			"connectedAt": c.ConnectedAt.Unix(),
		})
	}
	return out
}

func (s *Server) fanOutJSON(address string, payload map[string]interface{}) (okCount int) {
	recipients := s.snapshotClientsForAddr(address)
	var dead []*Client
	for _, c := range recipients {
		c.mu.Lock()
		err := c.Conn.WriteJSON(payload)
		c.mu.Unlock()
		if err != nil {
			dead = append(dead, c)
			continue
		}
		okCount++
	}
	if len(dead) > 0 {
		s.mu.Lock()
		for _, c := range dead {
			if c.Address != "" {
				s.removeClientFromAddrLocked(c.Address, c.ID)
			}
			delete(s.clients, c.ID)
		}
		s.mu.Unlock()
	}
	return okCount
}

func (s *Server) broadcastDevicesUpdate(address string) {
	if address == "" {
		return
	}
	s.mu.RLock()
	devices := s.deviceEntriesForAddrLocked(address)
	recipients := s.snapshotClientsForAddrLocked(address)
	s.mu.RUnlock()
	payload := map[string]interface{}{
		"type": "devices:update",
		"data": map[string]interface{}{
			"address":     address,
			"deviceCount": len(devices),
			"devices":     devices,
		},
	}
	for _, c := range recipients {
		c.mu.Lock()
		_ = c.Conn.WriteJSON(payload)
		c.mu.Unlock()
	}
}

// handleClientRegister handles client address registration (multi-device safe).
func (s *Server) handleClientRegister(client *Client, data map[string]interface{}) {
	addressRaw, ok := data["address"].(string)
	if !ok || addressRaw == "" {
		log.Printf("[MESSAGE] Invalid address registration from client %s", client.ID)
		return
	}
	address := normalizePlatariumAddress(addressRaw)

	deviceID, _ := data["deviceId"].(string)
	deviceLabel, _ := data["deviceLabel"].(string)
	deviceID = strings.TrimSpace(deviceID)
	deviceLabel = strings.TrimSpace(deviceLabel)
	if deviceID == "" {
		deviceID = client.ID
	}
	if deviceLabel == "" {
		deviceLabel = "Device"
	}

	var announceAddr, announcePk string

	s.mu.Lock()
	if client.Address != "" {
		old := normalizePlatariumAddress(client.Address)
		if old != address {
			s.removeClientFromAddrLocked(old, client.ID)
		}
	}
	client.Address = address
	client.DeviceID = deviceID
	client.DeviceLabel = deviceLabel
	s.addClientToAddrLocked(address, client)
	if pk, ok := data["e2eePublicKey"].(string); ok && pk != "" {
		s.e2eePubKeys[address] = pk
		announceAddr, announcePk = address, pk
	}
	pending := s.offlineMessages[address]
	if len(pending) == 0 && addressRaw != address {
		pending = s.offlineMessages[addressRaw]
		delete(s.offlineMessages, addressRaw)
	}
	delete(s.offlineMessages, address)
	devices := s.deviceEntriesForAddrLocked(address)
	s.mu.Unlock()

	now := time.Now().Unix()
	filtered := pending[:0]
	for _, m := range pending {
		if offlineMessageAgeOK(m, now) {
			filtered = append(filtered, m)
		}
	}
	pending = filtered

	log.Printf("[MESSAGE] Client %s registered address: %s device=%s (%s) sessions=%d",
		client.ID[:8], address, deviceID, deviceLabel, len(devices))

	client.mu.Lock()
	_ = client.Conn.WriteJSON(map[string]interface{}{
		"type": "registered",
		"data": map[string]interface{}{
			"address":          address,
			"deviceId":         deviceID,
			"deviceLabel":      deviceLabel,
			"deviceCount":      len(devices),
			"devices":          devices,
			"ownDevicesOnline": devices,
		},
	})
	client.mu.Unlock()

	if len(pending) > 0 {
		log.Printf("[MESSAGE] Delivering %d buffered message(s) to %s (device %s)", len(pending), address, deviceID)
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

	s.broadcastDevicesUpdate(address)

	if announcePk != "" {
		go s.broadcastE2eePubKeyAnnouncement(announceAddr, announcePk)
	}
}

func (s *Server) handleDevicesList(client *Client) {
	addr := client.Address
	if addr == "" {
		return
	}
	s.mu.RLock()
	devices := s.deviceEntriesForAddrLocked(addr)
	s.mu.RUnlock()
	client.mu.Lock()
	_ = client.Conn.WriteJSON(map[string]interface{}{
		"type": "devices:update",
		"data": map[string]interface{}{
			"address":     addr,
			"deviceCount": len(devices),
			"devices":     devices,
		},
	})
	client.mu.Unlock()
}

func (s *Server) handleDevicesLogout(client *Client, data map[string]interface{}) {
	if client.Address == "" {
		return
	}
	targetDeviceID, _ := data["deviceId"].(string)
	targetDeviceID = strings.TrimSpace(targetDeviceID)
	if targetDeviceID == "" {
		return
	}
	addr := client.Address

	s.mu.Lock()
	var target *Client
	for _, c := range s.clientsByAddr[addr] {
		if c != nil && c.DeviceID == targetDeviceID {
			target = c
			break
		}
	}
	s.mu.Unlock()

	if target == nil {
		client.mu.Lock()
		_ = client.Conn.WriteJSON(map[string]interface{}{
			"type": "devices:logoutResult",
			"data": map[string]interface{}{"ok": false, "error": "device_not_found"},
		})
		client.mu.Unlock()
		return
	}

	target.mu.Lock()
	_ = target.Conn.WriteJSON(map[string]interface{}{
		"type": "forceLogout",
		"data": map[string]interface{}{
			"reason":   "remote_logout",
			"deviceId": targetDeviceID,
		},
	})
	_ = target.Conn.Close()
	target.mu.Unlock()

	client.mu.Lock()
	_ = client.Conn.WriteJSON(map[string]interface{}{
		"type": "devices:logoutResult",
		"data": map[string]interface{}{"ok": true, "deviceId": targetDeviceID},
	})
	client.mu.Unlock()
}

// handleDirectMessage routes a message to all online devices of the recipient.
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

	recipients := s.snapshotClientsForAddr(to)
	if len(recipients) == 0 && toRaw != to {
		recipients = s.snapshotClientsForAddr(toRaw)
	}

	writeMessageSent := func(delivered bool, deviceCount int, devices []map[string]interface{}) {
		s.mu.RLock()
		ownDevices := s.deviceEntriesForAddrLocked(from)
		s.mu.RUnlock()
		sender.Conn.WriteJSON(map[string]interface{}{
			"type": "messageSent",
			"data": map[string]interface{}{
				"to":               to,
				"timestamp":        time.Now().Unix(),
				"delivered":        delivered,
				"deviceCount":      deviceCount,
				"devices":          devices,
				"ownDevicesOnline": ownDevices,
			},
		})
	}

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

	if len(recipients) == 0 {
		log.Printf("[MESSAGE] Recipient %s offline on node, buffering (from %s)", to, from)
		s.mu.Lock()
		buf := s.offlineMessages[to]
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

		if s.nodesManager != nil {
			s.routeMessageToPeer(to, from, text)
		}

		writeMessageSent(false, 0, nil)
		return
	}

	okCount := s.fanOutJSON(to, message)
	s.mu.RLock()
	devices := s.deviceEntriesForAddrLocked(to)
	s.mu.RUnlock()

	if okCount == 0 {
		log.Printf("[MESSAGE] All sessions dead for %s; buffering", to)
		s.mu.Lock()
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
		writeMessageSent(false, 0, nil)
		return
	}

	log.Printf("[MESSAGE] Message delivered from %s to %s (%d/%d devices)", from, to, okCount, len(recipients))
	writeMessageSent(true, okCount, devices)

	// Mirror to sender's other devices so multi-device outbox stays in sync.
	if from != to {
		s.mu.RLock()
		own := s.snapshotClientsForAddrLocked(from)
		s.mu.RUnlock()
		for _, c := range own {
			if c == nil || c.ID == sender.ID {
				continue
			}
			c.mu.Lock()
			_ = c.Conn.WriteJSON(message)
			c.mu.Unlock()
		}
	}
}

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

func (s *Server) routeMessageToPeer(to, from, text string) {
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

func (s *Server) HandleIncomingPeerMessage(data map[string]interface{}) {
	toRaw, _ := data["to"].(string)
	fromRaw, _ := data["from"].(string)
	text, _ := data["text"].(string)
	if toRaw == "" || fromRaw == "" || text == "" {
		return
	}
	to := normalizePlatariumAddress(toRaw)
	from := normalizePlatariumAddress(fromRaw)

	message := map[string]interface{}{
		"type": "message",
		"data": map[string]interface{}{
			"from":      from,
			"to":        to,
			"text":      text,
			"timestamp": data["timestamp"],
		},
	}

	ok := s.fanOutJSON(to, message)
	if ok > 0 {
		log.Printf("[MESSAGE] Message routed from peer: %s -> %s (%d devices)", from, to, ok)
		return
	}

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

func (s *Server) LookupE2eePubKey(address string) string {
	norm := normalizePlatariumAddress(address)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if pk := s.e2eePubKeys[norm]; pk != "" {
		return pk
	}
	return s.e2eePubKeys[address]
}

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
	if s.fanOutJSON(to, payload) > 0 {
		return
	}
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

func (s *Server) NotifyContactResolved(req contacteconomy.ContactRequest, encryptedResponse string) {
	payload := map[string]interface{}{
		"type": "contactResolved",
		"data": map[string]interface{}{
			"requestId":         req.RequestID,
			"status":            req.Status,
			"settleOutcome":     req.SettleOutcome,
			"sender":            req.Sender,
			"receiver":          req.Receiver,
			"encryptedResponse": encryptedResponse,
		},
	}
	for _, addr := range []string{req.Sender, req.Receiver} {
		a := normalizePlatariumAddress(addr)
		_ = s.fanOutJSON(a, payload)
	}
}
