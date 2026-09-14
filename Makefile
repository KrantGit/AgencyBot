.PHONY: proto build test lint up down logs migrate-up migrate-down

proto:
	protoc -I proto --go_out=paths=source_relative:gen/go --go-grpc_out=paths=source_relative:gen/go proto/user/v1/user.proto proto/order/v1/order.proto
build:
	go build ./cmd/...
test:
	go test ./...
lint:
	go vet ./...
up:
	docker compose up --build
down:
	docker compose down
logs:
	docker compose logs -f
migrate-up:
	docker compose run --rm users-migrate && docker compose run --rm orders-migrate && docker compose run --rm notifications-migrate
migrate-down:
	docker compose run --rm users-migrate -path=/migrations -database=postgres://app:app@users-postgres:5432/users?sslmode=disable down 1
