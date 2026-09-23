package config

import "os"

type UserService struct{ DatabaseDSN, JWTSecret, GRPCPort, MetricsPort string }
type OrderService struct{ DatabaseDSN, UserGRPCAddr, JWTSecret, GRPCPort, MetricsPort string }
type BotService struct{ TelegramToken, UserGRPCAddr, OrderGRPCAddr, JWTSecret, MetricsPort string }
type NotificationService struct{ DatabaseDSN, KafkaBrokers, TelegramToken, MetricsPort string }

func User() UserService {
	return UserService{DatabaseDSN: env("USER_DB_DSN", "postgres://app:app@localhost:5433/users?sslmode=disable"), JWTSecret: env("JWT_SECRET", "development-only-change-me"), GRPCPort: env("GRPC_PORT", "50051"), MetricsPort: env("METRICS_PORT", "8080")}
}
func Order() OrderService {
	return OrderService{DatabaseDSN: env("ORDER_DB_DSN", "postgres://app:app@localhost:5434/orders?sslmode=disable"), UserGRPCAddr: env("USER_GRPC_ADDR", "localhost:50051"), JWTSecret: env("JWT_SECRET", "development-only-change-me"), GRPCPort: env("GRPC_PORT", "50052"), MetricsPort: env("METRICS_PORT", "8080")}
}
func Bot() BotService {
	return BotService{TelegramToken: os.Getenv("TELEGRAM_BOT_TOKEN"), UserGRPCAddr: env("USER_GRPC_ADDR", "localhost:50051"), OrderGRPCAddr: env("ORDER_GRPC_ADDR", "localhost:50052"), JWTSecret: env("JWT_SECRET", "development-only-change-me"), MetricsPort: env("METRICS_PORT", "8080")}
}
func Notification() NotificationService {
	return NotificationService{DatabaseDSN: env("NOTIFICATION_DB_DSN", "postgres://app:app@localhost:5435/notifications?sslmode=disable"), KafkaBrokers: env("KAFKA_BROKERS", "localhost:9092"), TelegramToken: os.Getenv("TELEGRAM_BOT_TOKEN"), MetricsPort: env("METRICS_PORT", "8080")}
}
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
