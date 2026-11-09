// Command auth menjalankan AuthService (gRPC) Ice-Stream.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	authv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/auth/v1"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/auth"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/config"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/gormdb"
	"github.com/farhantaufikrahman/ice-stream/backend/internal/grpcserver"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("auth service berhenti karena error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.LoadAuth()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	key, err := auth.LoadPrivateKey(cfg.JWTPrivateKeyFile)
	if err != nil {
		return err
	}

	db, sqlDB, err := gormdb.Open(cfg.DB)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	svc, err := auth.NewService(auth.NewOracleRepository(db), auth.NewTokenMaker(key, cfg.AccessTokenTTL), cfg.RefreshTokenTTL)
	if err != nil {
		return err
	}

	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.LogUnary(logger)))
	authv1.RegisterAuthServiceServer(srv, auth.NewGRPCServer(svc, logger))

	// Health check standar gRPC, dipakai probe Kubernetes dan grpcurl.
	// Statusnya NOT_SERVING selama Oracle tidak bisa dihubungi.
	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(srv, healthSrv)
	go grpcserver.WatchHealth(ctx, healthSrv, authv1.AuthService_ServiceDesc.ServiceName, []grpcserver.Check{
		{Name: "oracle", Run: sqlDB.PingContext},
	}, logger)

	// Reflection membuat grpcurl bisa melihat daftar RPC tanpa file .proto.
	// Hanya dinyalakan di development.
	if !cfg.IsProduction() {
		reflection.Register(srv)
	}

	logger.Info("auth service siap", slog.String("env", cfg.Env))
	return grpcserver.Serve(ctx, srv, healthSrv, cfg.GRPCAddr, cfg.ShutdownTimeout, logger)
}
