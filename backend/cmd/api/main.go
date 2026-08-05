package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fethiaksit/social-analytics/internal/app"
)

func main() {
	application, err := app.New()
	if err != nil {
		log.Fatal(err)
	}
	go application.Scheduler.Start()
	go func() {
		if err := application.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("server stopped: %v", err)
		}
	}()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	application.Scheduler.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := application.Server.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
