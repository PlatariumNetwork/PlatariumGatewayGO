package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/handlers"
	"platarium-gateway-go/internal/logger"
	"platarium-gateway-go/internal/nodes"
	"platarium-gateway-go/internal/websocket"

	"github.com/gorilla/mux"
)

var (
	portREST = flag.Int("port", 1812, "REST API port")
	portWS   = flag.Int("ws", 1813, "WebSocket server port")
	testnet  = flag.Bool("testnet", false, "Test network mode: requires Platarium Core (platarium-cli) for transaction validation; rejects TX without valid signature")
)

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "localhost"
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "localhost"
}

func main() {
	flag.Parse()

	log.Println("[Logging activated]")
	if *testnet {
		log.Println("[TESTNET] Test network mode: Core validation required for transactions")
	}
	log.Printf("Starting Platarium Gateway on REST:%d, WS:%d", *portREST, *portWS)

	// Initialize blockchain
	bc := blockchain.NewBlockchain()
	if err := bc.Init(); err != nil {
		log.Fatalf("Failed to initialize blockchain: %v", err)
	}
	log.Println("Blockchain initialized for REST API")

	// Get node host
	nodeHost := os.Getenv("NODE_HOST")
	if nodeHost == "" {
		nodeHost = getLocalIP()
	}

	// Initialize nodes manager
	nodesManager := nodes.NewNodesManager(*portWS, nodeHost)
	nodesManager.SetRestBaseURL("http://" + nodeHost + ":" + strconv.Itoa(*portREST))
	logger.SetNodeID(nodesManager.GetNodeID())
	log.Printf("[NODE] Initialized node: %s at ws://%s:%d", nodesManager.GetNodeID(), nodeHost, *portWS)

	// Initialize WebSocket server
	wsServer := websocket.NewServer(*portWS, bc, nodesManager)

	// Setup REST API
	router := mux.NewRouter()
	router.Use(corsMiddleware)
	router.Use(loggingMiddleware)

	// Initialize handlers (testnet mode requires Core for TX validation)
	handler, err := handlers.NewHandler(bc, nodesManager, wsServer, *testnet)
	if err != nil {
		log.Fatalf("Failed to create handler: %v", err)
	}

	// Static file server for web UI
	router.PathPrefix("/web/").Handler(http.StripPrefix("/web/", http.FileServer(http.Dir("./web/"))))
	
	// API Routes (must be registered before root handler)
	router.HandleFunc("/api", handler.HealthCheck).Methods("GET")
	router.HandleFunc("/network", handler.NetworkStatus).Methods("GET")
	router.HandleFunc("/sockets", handler.GetSockets).Methods("GET")
	
	// RPC endpoints for monitoring (must be registered before root handler)
	router.HandleFunc("/rpc/status", handler.GetDetailedStatus).Methods("GET")
	router.HandleFunc("/rpc/sockets", handler.GetSockets).Methods("GET")
	router.HandleFunc("/rpc/ping", handler.PingPeer).Methods("GET")
	
	// Blockchain API routes
	router.HandleFunc("/pg-bal/{address}", handler.GetBalance).Methods("GET")
	router.HandleFunc("/pg-tx/{hash}", handler.GetTransaction).Methods("GET")
	router.HandleFunc("/pg-alltx/{address}", handler.GetTransactions).Methods("GET")
	router.HandleFunc("/pg-sendtx", handler.SendTransaction).Methods("POST")
	// Demo UI: mempool, list all TX, demo send (mempool only), confirm block
	router.HandleFunc("/api/mempool", handler.GetMempool).Methods("GET")
	router.HandleFunc("/api/transactions", handler.GetAllTransactions).Methods("GET")
	router.HandleFunc("/api/blocks", handler.GetBlockHistory).Methods("GET")
	router.HandleFunc("/api/block/{blockNumber}", handler.GetBlock).Methods("GET")
	router.HandleFunc("/api/stats", handler.GetStats).Methods("GET")
	router.HandleFunc("/api/demo-sendtx", handler.DemoSendTx).Methods("POST")
	router.HandleFunc("/api/confirm-block", handler.ConfirmBlock).Methods("POST")
	router.HandleFunc("/api/pending-block", handler.GetPendingBlock).Methods("GET")
	router.HandleFunc("/api/l1-collect", handler.L1CollectBlock).Methods("POST")
	router.HandleFunc("/api/l2-confirm", handler.L2ConfirmBlock).Methods("POST")
	router.HandleFunc("/api/reward-config", handler.GetRewardConfig).Methods("GET")
	router.HandleFunc("/api/reward-credit-l1", handler.RewardCreditL1).Methods("POST")
	router.HandleFunc("/api/fee-distribution", handler.GetFeeDistribution).Methods("GET")
	router.HandleFunc("/api/node-ratings", handler.GetNodeRatings).Methods("GET")
	router.HandleFunc("/api/last-votes", handler.GetLastVotes).Methods("GET")
	router.HandleFunc("/api/test-set-load", handler.TestSetLoad).Methods("POST")
	router.HandleFunc("/api/generate-wallet", handler.GenerateWallet).Methods("GET")
	router.HandleFunc("/api/faucet", handler.Faucet).Methods("POST")

	// Serve index.html at root and /index.html (must be last to not interfere with other routes)
	router.HandleFunc("/index.html", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./web/index.html")
	}).Methods("GET")
	
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Only serve index.html for exact root path
		if r.URL.Path == "/" {
			http.ServeFile(w, r, "./web/index.html")
		} else {
			// For any other path, return 404
			http.NotFound(w, r)
		}
	}).Methods("GET")

	// Start REST API server
	restServer := &http.Server{
		Addr:    fmt.Sprintf("0.0.0.0:%d", *portREST), // Listen on all interfaces
		Handler: router,
	}

	go func() {
		log.Printf("[REST] REST API running on 0.0.0.0:%d (all interfaces)", *portREST)
		if err := restServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("REST API server error: %v", err)
		}
	}()

	// Start WebSocket server
	go func() {
		if err := wsServer.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("WebSocket server error: %v", err)
		}
	}()

	// Connect to peer nodes after a delay (for 100-node testnet use PLATARIUM_PEER_CONNECT_DELAY_SEC=8)
	peerDelaySec := 1
	if s := os.Getenv("PLATARIUM_PEER_CONNECT_DELAY_SEC"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			if n > 60 {
				n = 60
			}
			peerDelaySec = n
		}
	}
	peerDelay := time.Duration(peerDelaySec) * time.Second
	go func() {
		time.Sleep(peerDelay)
		log.Println("[NODE] Connecting to peer nodes...")
		nodesManager.ConnectToPeers()
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down servers...")
	wsServer.Stop()
	restServer.Close()
	log.Println("Servers stopped")
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[HTTP] %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

