package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/tyronleev526-gif/trade-analysis-backend/api"
	"github.com/tyronleev526-gif/trade-analysis-backend/auth"
	"github.com/tyronleev526-gif/trade-analysis-backend/cache"
	"github.com/tyronleev526-gif/trade-analysis-backend/database"
	"github.com/tyronleev526-gif/trade-analysis-backend/indicators"
	"github.com/tyronleev526-gif/trade-analysis-backend/market"
	"github.com/tyronleev526-gif/trade-analysis-backend/providers"
	binance "github.com/tyronleev526-gif/trade-analysis-backend/providers/binance"
	mock "github.com/tyronleev526-gif/trade-analysis-backend/providers/mock"
	"github.com/tyronleev526-gif/trade-analysis-backend/signals"
	"github.com/tyronleev526-gif/trade-analysis-backend/websocket"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// DB
	dbPool, err := database.NewPostgres(os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer dbPool.Close(context.Background())

	// Run migrations
	if err := database.RunMigrations(context.Background(), dbPool); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	// Redis
	rdb := cache.NewRedis(envOrDefault("REDIS_ADDR", "redis:6379"), os.Getenv("REDIS_PASSWORD"))
	defer rdb.Close()

	// Services
	indicatorSvc := indicators.NewService()
	signalEngine := signals.NewEngine(indicatorSvc)

	// Websocket hub
	hub := websocket.NewHub()
	go hub.Run()

	// Processor
	mp := market.NewProcessor(dbPool, rdb, indicatorSvc, signalEngine, hub)

	// Provider: use Binance by default, use mock when USE_MOCK=true
	var provider providers.Provider
	useMock := strings.EqualFold(os.Getenv("USE_MOCK"), "true")
	symbolsEnv := os.Getenv("SYMBOLS")
	var symbols []string
	if symbolsEnv != "" {
		for _, s := range strings.Split(symbolsEnv, ",") {
			symbols = append(symbols, strings.ToUpper(strings.TrimSpace(s)))
		}
	} else {
		symbols = []string{"BTCUSDT", "ETHUSDT"}
	}

	if useMock {
		provider = mock.NewMockProvider()
		log.Println("using mock provider (USE_MOCK=true)")
	} else {
		provider = binance.NewBinanceProvider(symbols)
		log.Println("using Binance provider")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go provider.Start(ctx, mp.InputChannel())

	// Auth service
	authSvc := auth.NewService(dbPool)

	// HTTP API
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	api.RegisterRoutes(router, authSvc, mp, indicatorSvc, signalEngine, hub)
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	// graceful shutdown
	go func() {
		log.Printf("server listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("server stopped: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")

	ctxShutdown, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()

	// Stop provider/processor/hub
	cancel()
	_ = provider.Stop()
	mp.Stop(ctxShutdown)
	hub.Shutdown()

	if err := srv.Shutdown(ctxShutdown); err != nil {
		log.Printf("server graceful shutdown error: %v", err)
	}

	log.Println("done")
}

func envOrDefault(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}
