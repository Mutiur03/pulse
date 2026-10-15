package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Mutiur03/pulse/internal/config"
)

func main() {
	cfg := config.Load()
	log.Printf("Platform combined binary starting with config: %+v", cfg)

	log.Println("This would start all services in one process")
	log.Println("For now, run services separately")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
}
