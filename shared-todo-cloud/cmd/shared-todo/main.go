package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"autodevjapan/shared-todo-cloud/internal/httpapi"
	"autodevjapan/shared-todo-cloud/internal/store"
)

func main() {
	dir := os.Getenv("TODO_DATA_DIR")
	if dir == "" {
		fmt.Fprintln(os.Stderr, "TODO_DATA_DIR is required for durable storage")
		os.Exit(1)
	}
	if os.Getenv("RAILWAY_PROJECT_ID") != "" {
		mount := os.Getenv("RAILWAY_VOLUME_MOUNT_PATH")
		dataPath, dataErr := filepath.Abs(dir)
		mountPath, mountErr := filepath.Abs(mount)
		if mount == "" || dataErr != nil || mountErr != nil || dataPath != mountPath {
			fmt.Fprintln(os.Stderr, "Railway persistent volume must be mounted at TODO_DATA_DIR")
			os.Exit(1)
		}
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		fmt.Fprintln(os.Stderr, "cannot create the configured data directory")
		os.Exit(1)
	}
	s, e := store.Open(filepath.Join(dir, "shared-todo.db"))
	if e != nil {
		fmt.Fprintln(os.Stderr, "cannot open durable task storage")
		os.Exit(1)
	}
	defer s.Close()
	publicURL := os.Getenv("TODO_PUBLIC_URL")
	var departmentOrder []string
	if configured := os.Getenv("TODO_DEPARTMENT_ORDER"); configured != "" {
		if json.Unmarshal([]byte(configured), &departmentOrder) != nil {
			fmt.Fprintln(os.Stderr, "TODO_DEPARTMENT_ORDER must be a JSON array")
			os.Exit(1)
		}
	}
	secure := os.Getenv("TODO_ALLOW_LOCAL_HTTP") != "1"
	app, e := httpapi.New(s, httpapi.Config{AdminToken: os.Getenv("TODO_ADMIN_TOKEN"), SyncToken: os.Getenv("TODO_SYNC_TOKEN"), PublicURL: publicURL, SecureCookies: secure, DepartmentOrder: departmentOrder})
	if e != nil {
		fmt.Fprintln(os.Stderr, e.Error())
		os.Exit(1)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	server := &http.Server{Addr: net.JoinHostPort(os.Getenv("TODO_LISTEN_HOST"), port), Handler: app, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute, WriteTimeout: 5 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	stop := make(chan os.Signal, 1)
	shutdownDone := make(chan struct{})
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer close(shutdownDone)
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	fmt.Println("shared-todo listening on configured port")
	if e = server.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "HTTP server stopped unexpectedly")
		os.Exit(1)
	}
	<-shutdownDone
}
