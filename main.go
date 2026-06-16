package main

import (
	"bufio"
	"context"
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/store"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	httpPort := flag.Int("port", 8899, "port to listen on")
	dataDir := flag.String("data", "./data", "directory to store data")
	flag.Parse()

	status := run(ctx, cancel, *httpPort, *dataDir)
	cancel()
	os.Exit(status)
}

func run(ctx context.Context, cancel context.CancelFunc, httpPort int, dataDir string) int {
	file, err := os.OpenFile("linko.access.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return 1
	}
	defer file.Close()

	logger := initializeLogger()
	st, err := store.New(dataDir)
	if err != nil {
		logger.Printf("failed to create store: %v", err)
		return 1
	}
	s := newServer(*st, httpPort, cancel, logger)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		logger.Printf("failed to shutdown server: %v", err)
		return 1
	}
	if serverErr != nil {
		logger.Printf("server error: %v", serverErr)
		return 1
	}
	return 0
}

func initializeLogger() *log.Logger {

	if LINKO_LOG_FILE := os.Getenv("LINKO_LOG_FILE"); LINKO_LOG_FILE != "" {

		file, err := os.OpenFile(LINKO_LOG_FILE, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err != nil {
			log.Fatalf("failed to open log file: %v", err)
		}
		bufferedFile := bufio.NewWriterSize(file, 8192)
		multiWriter := io.MultiWriter(os.Stderr, bufferedFile)
		return log.New(multiWriter, "", log.LstdFlags)
	} else {
		return log.New(os.Stderr, "", log.LstdFlags)
	}
}
