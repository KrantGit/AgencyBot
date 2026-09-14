package config

import "os"

type UserService struct{ DatabaseDSN, JWTSecret, GRPCPort, MetricsPort string }

func User() UserService {
	return UserService{DatabaseDSN: env("USER_DB_DSN", "postgres://app:app@localhost:5433/users?sslmode=disable"), JWTSecret: env("JWT_SECRET", "development-only-change-me"), GRPCPort: env("GRPC_PORT", "50051"), MetricsPort: env("METRICS_PORT", "8080")}
}
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
