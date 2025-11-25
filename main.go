package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"platarium-gateway-go/internal/blockchain"
	"platarium-gateway-go/internal/handlers"
	"platarium-gateway-go/internal/nodes"
	"platarium-gateway-go/internal/websocket"

	"github.com/gorilla/mux"
)

var (
	portREST = flag.Int("port", 1812, "REST API port")
	portWS   = flag.Int("ws", 1813, "WebSocket server port")
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
	log.Printf("[NODE] Initialized node: %s at ws://%s:%d", nodesManager.GetNodeID(), nodeHost, *portWS)

	// Initialize WebSocket server
	wsServer := websocket.NewServer(*portWS, bc, nodesManager)

	// Setup REST API
	router := mux.NewRouter()
	router.Use(corsMiddleware)

	// Initialize handlers
	handler := handlers.NewHandler(bc, nodesManager, wsServer)

	// Routes
	router.HandleFunc("/", handler.HealthCheck).Methods("GET")
	router.HandleFunc("/network", handler.NetworkStatus).Methods("GET")
	router.HandleFunc("/sockets", handler.GetSockets).Methods("GET")
	router.HandleFunc("/pg-bal/{address}", handler.GetBalance).Methods("GET")
	router.HandleFunc("/pg-tx/{hash}", handler.GetTransaction).Methods("GET")
	router.HandleFunc("/pg-alltx/{address}", handler.GetTransactions).Methods("GET")
	router.HandleFunc("/pg-sendtx", handler.SendTransaction).Methods("POST")

	// Start REST API server
	restServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", *portREST),
		Handler: router,
	}

	go func() {
		log.Printf("REST API running at http://localhost:%d", *portREST)
		if err := restServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("REST API server error: %v", err)
		}
	}()

	// Start WebSocket server
	go func() {
		if err := wsServer.Start(); err != nil {
			log.Fatalf("WebSocket server error: %v", err)
		}
	}()

	// Connect to peer nodes after a short delay
	go func() {
		time.Sleep(1 * time.Second)
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

