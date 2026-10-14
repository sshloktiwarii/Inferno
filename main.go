package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sshloktiwarii/inferno-telemetry-proxy/pkg/batcher"
	"github.com/sshloktiwarii/inferno-telemetry-proxy/pkg/hardware"
	"github.com/sshloktiwarii/inferno-telemetry-proxy/pkg/proxy"
	"github.com/sshloktiwarii/inferno-telemetry-proxy/pkg/telemetry"
)

// checkPortAvailable probes if a given TCP port can be bound.
func checkPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func main() {
	portFlag := flag.Int("port", 8085, "TCP port to serve Inferno telemetry proxy")
	batchWindowMs := flag.Int("batch-window-ms", 20, "Dynamic continuous batching window in milliseconds")
	batchLimit := flag.Int("batch-limit", 16, "Maximum micro-batch size")
	maxVramPressure := flag.Float64("max-vram-pressure", 90.0, "VRAM memory pressure threshold triggering backpressure")
	flag.Parse()

	targetPort := *portFlag
	if !checkPortAvailable(targetPort) {
		log.Printf("[INVARIANT WARNING] Port %d is occupied. Probing alternate ports...", targetPort)
		for p := 8086; p < 8095; p++ {
			if checkPortAvailable(p) {
				targetPort = p
				break
			}
		}
	}

	log.Printf("Initializing Inferno Telemetry Proxy on :%d...", targetPort)

	// 1. Initialize Dynamic Batcher
	dynBatcher := batcher.NewDynamicBatcher(time.Duration(*batchWindowMs)*time.Millisecond, *batchLimit)
	defer dynBatcher.Close()

	// 2. Initialize Hardware Telemetry & Backpressure Governor
	governor := hardware.NewBackpressureGovernor(*maxVramPressure)

	// 3. Initialize 3-Tier Latency Tracker
	tracker := telemetry.NewLatencyTracker()

	// 4. Initialize HTTP Proxy
	srvHandler := proxy.NewServer(dynBatcher, governor, tracker)

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", targetPort),
		Handler:      srvHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 5. Graceful Zero-Zombie Shutdown Handling (Systems Engineering Invariant)
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Inferno Telemetry Proxy listening at http://localhost:%d", targetPort)
		log.Printf("Endpoints: /v1/chat/completions (OpenAI compatible), /metrics (Prometheus), /health")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Proxy server failed: %v", err)
		}
	}()

	<-stopChan
	log.Println("Received termination signal. Shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Server forced shutdown: %v", err)
	}

	log.Println("Inferno proxy exited cleanly.")
}
