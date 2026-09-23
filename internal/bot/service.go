package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	orderv1 "github.com/example/order-platform/gen/go/order/v1"
	userv1 "github.com/example/order-platform/gen/go/user/v1"
	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/metadata"
)

type Service struct {
	telegram *TelegramClient
	users    userv1.UserServiceClient
	orders   orderv1.OrderServiceClient
	secret   string
}

func NewService(telegram *TelegramClient, users userv1.UserServiceClient, orders orderv1.OrderServiceClient, secret string) *Service {
	return &Service{telegram: telegram, users: users, orders: orders, secret: secret}
}

func (s *Service) Run(ctx context.Context) error {
	var offset int64
	for ctx.Err() == nil {
		pollCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
		updates, err := s.telegram.Updates(pollCtx, offset)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.Error("telegram polling failed", "error", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
			}
			continue
		}
		for _, update := range updates {
			offset = update.ID + 1
			if update.Message == nil || update.Message.Text == "" || update.Message.From.ID == 0 {
				continue
			}
			if err := s.handle(ctx, update); err != nil {
				_ = s.telegram.Send(ctx, update.Message.Chat.ID, "Не удалось обработать команду. Попробуйте ещё раз.")
			}
		}
	}
	return nil
}

func (s *Service) handle(ctx context.Context, update Update) error {
	text := strings.TrimSpace(update.Message.Text)
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return nil
	}
	command := strings.Split(parts[0], "@")[0]
	switch command {
	case "/start":
		if len(parts) == 2 {
			return s.bind(ctx, update.Message.Chat.ID, update.Message.From.ID, parts[1])
		}
		return s.sendWelcome(ctx, update.Message.Chat.ID, update.Message.From.ID)
	case "/orders":
		return s.sendOrders(ctx, update.Message.Chat.ID, update.Message.From.ID)
	case "/help":
		return s.telegram.Send(ctx, update.Message.Chat.ID, "Команды:\n/start <код> — привязать аккаунт\n/orders — мои активные заказы")
	default:
		return s.telegram.Send(ctx, update.Message.Chat.ID, "Неизвестная команда. Используйте /help.")
	}
}

func (s *Service) bind(ctx context.Context, chatID, telegramID int64, token string) error {
	_, err := s.users.RedeemTelegramLink(ctx, &userv1.RedeemTelegramLinkRequest{Token: token, TelegramId: telegramID})
	if err != nil {
		return s.telegram.Send(ctx, chatID, "Код привязки недействителен, истёк или уже был использован.")
	}
	return s.telegram.Send(ctx, chatID, "Аккаунт успешно привязан. Используйте /orders для просмотра заказов.")
}

func (s *Service) sendWelcome(ctx context.Context, chatID, telegramID int64) error {
	user, err := s.users.GetUserByTelegramID(s.auth(ctx, "telegram-bot", nil), &userv1.GetUserByTelegramIDRequest{TelegramId: telegramID})
	if err != nil || user.User == nil {
		return s.telegram.Send(ctx, chatID, "Чтобы начать, получите код привязки у администратора и отправьте /start <код>.")
	}
	return s.telegram.Send(ctx, chatID, "Здравствуйте, "+user.User.FullName+"! Используйте /orders для просмотра активных заказов.")
}

func (s *Service) sendOrders(ctx context.Context, chatID, telegramID int64) error {
	user, err := s.users.GetUserByTelegramID(s.auth(ctx, "telegram-bot", nil), &userv1.GetUserByTelegramIDRequest{TelegramId: telegramID})
	if err != nil || user.User == nil {
		return s.telegram.Send(ctx, chatID, "Сначала привяжите аккаунт: /start <код>.")
	}
	orders, err := s.orders.ListPerformerOrders(s.auth(ctx, user.User.Id, user.User.Permissions), &orderv1.ListPerformerOrdersRequest{PageSize: 20})
	if err != nil {
		return s.telegram.Send(ctx, chatID, "Не удалось получить список заказов.")
	}
	if len(orders.Orders) == 0 {
		return s.telegram.Send(ctx, chatID, "Активных заказов нет.")
	}
	lines := make([]string, 0, len(orders.Orders)+1)
	lines = append(lines, "Ваши активные заказы:")
	for _, order := range orders.Orders {
		lines = append(lines, fmt.Sprintf("• %s — %s, %s", order.OrderDate.AsTime().Format("02.01 15:04"), order.Location, order.Amount))
	}
	return s.telegram.Send(ctx, chatID, strings.Join(lines, "\n"))
}

func (s *Service) auth(ctx context.Context, subject string, permissions []string) context.Context {
	claims := jwt.MapClaims{"role": "BOT", "permissions": permissions, "sub": subject, "exp": time.Now().Add(5 * time.Minute).Unix()}
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.secret))
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}
