package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	userv1 "github.com/example/order-platform/gen/go/user/v1"
	"github.com/example/order-platform/internal/config"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func main() {
	userID := flag.String("user-id", "", "UUID пользователя")
	login := flag.String("create-admin", "", "логин нового администратора")
	fullName := flag.String("full-name", "", "полное имя нового администратора")
	password := flag.String("password", "", "пароль нового администратора")
	validFor := flag.Duration("expires-in", 15*time.Minute, "срок действия кода")
	flag.Parse()
	if *userID != "" && *login != "" {
		log.Fatal("use either -user-id or -create-admin")
	}
	cfg := config.Bot()
	if cfg.JWTSecret == "" {
		log.Fatal("JWT_SECRET is required")
	}
	conn, err := grpc.NewClient(cfg.UserGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	claims := jwt.MapClaims{"sub": "telegram-link-cli", "role": "ADMIN", "exp": time.Now().Add(time.Minute).Unix()}
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		log.Fatal(err)
	}
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+accessToken)
	client := userv1.NewUserServiceClient(conn)
	var id uuid.UUID
	if *login != "" {
		if *fullName == "" || *password == "" {
			log.Fatal("-full-name and -password are required with -create-admin")
		}
		created, err := client.CreateUser(ctx, &userv1.CreateUserRequest{Login: *login, FullName: *fullName, Password: *password, Role: userv1.Role_ROLE_ADMIN})
		if err != nil {
			log.Fatal(err)
		}
		id, err = uuid.Parse(created.User.Id)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		id, err = uuid.Parse(*userID)
		if err != nil {
			log.Fatal("-user-id must be a valid UUID")
		}
	}
	response, err := client.CreateTelegramLink(ctx, &userv1.CreateTelegramLinkRequest{UserId: id.String(), ExpiresInSeconds: uint32(validFor.Seconds())})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(response.Token)
}
