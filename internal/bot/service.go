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
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Service struct {
	telegram *TelegramClient
	users    userv1.UserServiceClient
	orders   orderv1.OrderServiceClient
	drafts   *DraftStore
	secret   string
}

func NewService(telegram *TelegramClient, users userv1.UserServiceClient, orders orderv1.OrderServiceClient, drafts *DraftStore, secret string) *Service {
	return &Service{telegram: telegram, users: users, orders: orders, drafts: drafts, secret: secret}
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
	if text == "/cancel" {
		return s.cancelDraft(ctx, update.Message.Chat.ID, update.Message.From.ID)
	}
	if draft, err := s.drafts.Load(ctx, update.Message.From.ID); err != nil {
		return err
	} else if draft != nil {
		return s.handleDraft(ctx, update.Message.Chat.ID, update.Message.From.ID, text, draft)
	}
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
	case "/neworder":
		return s.startDraft(ctx, update.Message.Chat.ID, update.Message.From.ID)
	case "/help":
		return s.telegram.Send(ctx, update.Message.Chat.ID, "Команды:\n/start <код> — привязать аккаунт\n/orders — мои активные заказы\n/neworder — создать заказ (администратор)\n/cancel — отменить создание")
	default:
		return s.telegram.Send(ctx, update.Message.Chat.ID, "Неизвестная команда. Используйте /help.")
	}
}

func (s *Service) startDraft(ctx context.Context, chatID, telegramID int64) error {
	user, err := s.userForTelegram(ctx, telegramID)
	if err != nil || user == nil {
		return s.telegram.Send(ctx, chatID, "Сначала привяжите аккаунт: /start <код>.")
	}
	if !hasPermission(user.Permissions, "ORDER_CREATE") {
		return s.telegram.Send(ctx, chatID, "Создавать заказы могут только администраторы.")
	}
	if err = s.drafts.Save(ctx, telegramID, &Draft{Step: "date"}); err != nil {
		return err
	}
	return s.telegram.Send(ctx, chatID, "Новый заказ. Укажите дату и время в формате ДД.ММ.ГГГГ ЧЧ:ММ, например 25.09.2026 14:30.")
}

func (s *Service) handleDraft(ctx context.Context, chatID, telegramID int64, text string, draft *Draft) error {
	if draft.Step == "confirm" {
		if text != "/confirm" {
			return s.telegram.Send(ctx, chatID, "Проверьте данные и отправьте /confirm или /cancel.")
		}
		return s.confirmDraft(ctx, chatID, telegramID, draft)
	}
	if text == "" {
		return s.telegram.Send(ctx, chatID, "Введите значение или /cancel.")
	}
	switch draft.Step {
	case "date":
		if _, err := time.Parse("02.01.2006 15:04", text); err != nil {
			return s.telegram.Send(ctx, chatID, "Неверный формат. Пример: 25.09.2026 14:30.")
		}
		draft.OrderDate, draft.Step = text, "location"
		text = "Укажите адрес или место выполнения."
	case "location":
		draft.Location, draft.Step, text = text, "amount", "Укажите сумму заказа."
	case "amount":
		draft.Amount, draft.Step, text = text, "customer_name", "Укажите имя клиента."
	case "customer_name":
		draft.CustomerName, draft.Step, text = text, "customer_phone", "Укажите телефон клиента."
	case "customer_phone":
		draft.CustomerPhone, draft.Step, text = text, "customer_contact", "Укажите дополнительный контакт клиента или '-' если его нет."
	case "customer_contact":
		if text != "-" {
			draft.CustomerContact = text
		}
		draft.Step, text = "comment", "Укажите комментарий или '-'."
	case "comment":
		if text != "-" {
			draft.Comment = text
		}
		draft.Step = "performers"
		return s.askPerformers(ctx, chatID, telegramID, draft)
	case "performers":
		ids, err := s.resolvePerformers(ctx, text)
		if err != nil {
			return s.telegram.Send(ctx, chatID, err.Error())
		}
		draft.PerformerIDs, draft.Step = ids, "confirm"
		if err = s.drafts.Save(ctx, telegramID, draft); err != nil {
			return err
		}
		return s.telegram.Send(ctx, chatID, draftSummary(draft)+"\n\nОтправьте /confirm для создания или /cancel для отмены.")
	}
	if err := s.drafts.Save(ctx, telegramID, draft); err != nil {
		return err
	}
	return s.telegram.Send(ctx, chatID, text)
}

func (s *Service) askPerformers(ctx context.Context, chatID, telegramID int64, draft *Draft) error {
	users, err := s.users.ListUsers(s.auth(ctx, "telegram-bot", []string{"USER_READ"}), &userv1.ListUsersRequest{PageSize: 100})
	if err != nil {
		return err
	}
	logins := make([]string, 0)
	for _, user := range users.Users {
		if user.Role == userv1.Role_ROLE_PERFORMER && user.IsActive {
			logins = append(logins, user.Login)
		}
	}
	if err = s.drafts.Save(ctx, telegramID, draft); err != nil {
		return err
	}
	if len(logins) == 0 {
		return s.telegram.Send(ctx, chatID, "Активных исполнителей нет. Отправьте '-' для создания без назначения.")
	}
	return s.telegram.Send(ctx, chatID, "Укажите логины исполнителей через запятую или '-'. Доступно: "+strings.Join(logins, ", "))
}

func (s *Service) resolvePerformers(ctx context.Context, value string) ([]string, error) {
	if value == "-" {
		return nil, nil
	}
	all, err := s.users.ListUsers(s.auth(ctx, "telegram-bot", []string{"USER_READ"}), &userv1.ListUsersRequest{PageSize: 100})
	if err != nil {
		return nil, fmt.Errorf("не удалось получить исполнителей")
	}
	byLogin := map[string]string{}
	for _, user := range all.Users {
		if user.Role == userv1.Role_ROLE_PERFORMER && user.IsActive {
			byLogin[strings.ToLower(user.Login)] = user.Id
		}
	}
	ids := make([]string, 0)
	for _, login := range strings.Split(value, ",") {
		id, ok := byLogin[strings.ToLower(strings.TrimSpace(login))]
		if !ok {
			return nil, fmt.Errorf("исполнитель %q не найден или неактивен", strings.TrimSpace(login))
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *Service) confirmDraft(ctx context.Context, chatID, telegramID int64, draft *Draft) error {
	user, err := s.userForTelegram(ctx, telegramID)
	if err != nil || user == nil || !hasPermission(user.Permissions, "ORDER_CREATE") {
		return s.telegram.Send(ctx, chatID, "Недостаточно прав для создания заказа.")
	}
	date, err := time.Parse("02.01.2006 15:04", draft.OrderDate)
	if err != nil {
		return err
	}
	created, err := s.orders.CreateOrder(s.auth(ctx, user.Id, user.Permissions), &orderv1.CreateOrderRequest{OrderDate: timestamppb.New(date), Location: draft.Location, Amount: draft.Amount, CustomerName: draft.CustomerName, CustomerPhone: draft.CustomerPhone, CustomerContact: draft.CustomerContact, Comment: draft.Comment, PerformerIds: draft.PerformerIDs})
	if err != nil {
		slog.Warn("order creation failed", "error", err)
		return s.telegram.Send(ctx, chatID, "Не удалось создать заказ. Проверьте данные или начните заново: /neworder.")
	}
	if err = s.drafts.Delete(ctx, telegramID); err != nil {
		return err
	}
	return s.telegram.Send(ctx, chatID, "Заказ создан: "+created.Order.Id)
}

func (s *Service) cancelDraft(ctx context.Context, chatID, telegramID int64) error {
	if err := s.drafts.Delete(ctx, telegramID); err != nil {
		return err
	}
	return s.telegram.Send(ctx, chatID, "Создание заказа отменено.")
}

func (s *Service) userForTelegram(ctx context.Context, telegramID int64) (*userv1.User, error) {
	response, err := s.users.GetUserByTelegramID(s.auth(ctx, "telegram-bot", []string{"USER_READ"}), &userv1.GetUserByTelegramIDRequest{TelegramId: telegramID})
	if err != nil || response.User == nil {
		return nil, err
	}
	return response.User, nil
}

func hasPermission(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func draftSummary(draft *Draft) string {
	return fmt.Sprintf("Проверьте заказ:\nДата: %s\nМесто: %s\nСумма: %s\nКлиент: %s\nТелефон: %s\nКонтакт: %s\nКомментарий: %s\nИсполнителей: %d", draft.OrderDate, draft.Location, draft.Amount, draft.CustomerName, draft.CustomerPhone, draft.CustomerContact, draft.Comment, len(draft.PerformerIDs))
}

func (s *Service) bind(ctx context.Context, chatID, telegramID int64, token string) error {
	_, err := s.users.RedeemTelegramLink(ctx, &userv1.RedeemTelegramLinkRequest{Token: token, TelegramId: telegramID})
	if err != nil {
		slog.Warn("telegram link redemption failed", "error", err)
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
