// Command server runs the HTTP API, or applies migrations: `server migrate up|down`.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"ticketbooking/internal/cache"
	"ticketbooking/internal/config"
	"ticketbooking/internal/db"
	"ticketbooking/internal/handler"
	"ticketbooking/internal/middleware"
	"ticketbooking/internal/repository"
	"ticketbooking/internal/service"
)

func main() {
	_ = godotenv.Load() // optional: a .env file in the working directory

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if len(os.Args) < 3 {
			log.Fatal("usage: server migrate up|down")
		}
		if err := db.Migrate(cfg.DatabaseURL, os.Args[2]); err != nil {
			log.Fatal(err)
		}
		log.Printf("migrate %s: done", os.Args[2])
		return
	}

	gdb, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database error: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		log.Fatalf("database error: %v", err)
	}

	rdb, err := cache.Connect(cfg.RedisURL)
	if err != nil {
		log.Fatalf("redis config error: %v", err)
	}

	r := gin.New()
	r.Use(gin.Recovery(), middleware.RequestID(), middleware.CORS(cfg.CORSOrigin))
	r.GET("/healthz", handler.Health(
		func(ctx context.Context) error { return sqlDB.PingContext(ctx) },
		func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
	))
	catalogRepo := repository.NewCatalogRepository(gdb)
	catalogSvc := service.NewCatalogService(catalogRepo, rdb)
	catalogH := handler.NewCatalogHandler(catalogSvc)
	v1 := r.Group("/api/v1")
	v1.GET("/events", catalogH.ListEvents)
	v1.GET("/events/:id", catalogH.GetEvent)
	v1.GET("/showtimes/:id/seats", catalogH.GetShowtimeSeats)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	_ = rdb.Close()
	_ = sqlDB.Close()
}
