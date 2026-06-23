package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/build"

	"boot.dev/linko/internal/linkoerr"
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

	LINKO_LOG_FILE := os.Getenv("LINKO_LOG_FILE")
	env := os.Getenv("ENV")
	hostname, _ := os.Hostname()

	shutdown, err := initTracing(ctx)
	if err != nil {
		slog.Error("failed to initialize tracing: %v", err)
	}
	defer shutdown(ctx)

	logger, closeFn, initErr := initializeLogger(LINKO_LOG_FILE)
	logger = logger.With(
		slog.String("git_sha", build.GitSHA),
		slog.String("build_time", build.BuildTime),
		slog.String("env", env),
		slog.String("hostname", hostname),
	)
	if initErr != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	defer func() {
		if err := closeFn(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to cleanup: %v\n", err)
		}
	}()

	st, err := store.New(dataDir)
	if err != nil {
		logger.Error("failed to create store", slog.Any("error", err))
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
		logger.Error("failed to shutdown server", slog.Any("error", err))
		return 1
	}
	if serverErr != nil {
		logger.Error("server error", slog.Any("error", serverErr))
		return 1
	}
	return 0
}

type closeFunc func() error

// New helper function extracting single-error attribute building logic
func errorAttrs(err error) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("message", err.Error()),
	}

	if stackErr, ok := errors.AsType[stackTracer](err); ok {
		attrs = append(attrs, slog.String("stack_trace", fmt.Sprintf("%+v", stackErr.StackTrace())))
	}

	extractedAttrs := linkoerr.Attrs(err)
	attrs = append(attrs, extractedAttrs...)

	return attrs
}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if a.Key == "error" {
		err, ok := a.Value.Any().(error)
		if !ok {
			return a
		}

		// 1. Detect multi-errors
		if multiErr, ok := errors.AsType[multiError](err); ok {
			var errAttrs []slog.Attr

			for i, e := range multiErr.Unwrap() {
				// errorAttrs(e) extracts the slice of slog.Attr (message, path, etc.)
				// We group them under "error_1", "error_2" so they become structured nested objects
				errAttrs = append(errAttrs, slog.GroupAttrs(fmt.Sprintf("error_%d", i+1), errorAttrs(e)...))
			}

			// Return a top-level "errors" object grouping all the sub-objects
			return slog.GroupAttrs("errors", errAttrs...)
		}

		// 2. Fall back to treating it as a single error
		groupAttrs := errorAttrs(err)
		return slog.GroupAttrs("error", groupAttrs...)
	}
	return a
}

func initializeLogger(logFile string) (*slog.Logger, closeFunc, error) {

	debugHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:       slog.LevelDebug,
		ReplaceAttr: replaceAttr,
	})

	if logFile != "" {

		file, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open log file: %w", err)
		}
		bufferedFile := bufio.NewWriterSize(file, 8192)
		infoHandler := slog.NewJSONHandler(bufferedFile, &slog.HandlerOptions{
			Level:       slog.LevelInfo,
			ReplaceAttr: replaceAttr,
		})
		return slog.New(slog.NewMultiHandler(debugHandler, infoHandler)), func() error {
			if err := bufferedFile.Flush(); err != nil {
				return err
			}
			return file.Close()
		}, nil
	} else {
		return slog.New(debugHandler), func() error { return nil }, nil
	}
}

type multiError interface {
	error
	Unwrap() []error
}
