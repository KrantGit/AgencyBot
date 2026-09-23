package bot

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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
		if len(parts) == 3 {
			return s.bindWithPassword(ctx, update.Message.Chat.ID, update.Message.From.ID, parts[1], parts[2])
		}
		return s.telegram.Send(ctx, update.Message.Chat.ID, "Для привязки отправьте: /start <логин> <пароль>")
	case "/orders":
		return s.sendOrders(ctx, update.Message.Chat.ID, update.Message.From.ID)
	case "/ordersdate":
		if len(parts) != 2 {
			return s.telegram.Send(ctx, update.Message.Chat.ID, "Использование: /ordersdate ДД.ММ.ГГГГ")
		}
		return s.sendOrdersForDate(ctx, update.Message.Chat.ID, update.Message.From.ID, parts[1])
	case "/neworder":
		return s.startDraft(ctx, update.Message.Chat.ID, update.Message.From.ID)
	case "/newuser":
		return s.startUserDraft(ctx, update.Message.Chat.ID, update.Message.From.ID)
	case "/editorder":
		if len(parts) != 2 {
			return s.telegram.Send(ctx, update.Message.Chat.ID, "Использование: /editorder <ID заказа>")
		}
		return s.startEditDraft(ctx, update.Message.Chat.ID, update.Message.From.ID, parts[1])
	case "/help":
		return s.telegram.Send(ctx, update.Message.Chat.ID, "Команды:\n/start <логин> <пароль> — привязать аккаунт\n/orders — мои активные заказы\n/ordersdate ДД.ММ.ГГГГ — все заказы за дату\n/neworder — создать заказ\n/newuser — создать пользователя\n/editorder <ID> — изменить заказ\n/cancel — отменить действие")
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
	if err = s.drafts.Save(ctx, telegramID, &Draft{Kind: "order", Step: "date"}); err != nil {
		return err
	}
	return s.telegram.Send(ctx, chatID, "Новый заказ. Укажите дату и время в формате ДД.ММ.ГГГГ ЧЧ:ММ, например 25.09.2026 14:30.")
}

func (s *Service) handleDraft(ctx context.Context, chatID, telegramID int64, text string, draft *Draft) error {
	if draft.Kind == "user" {
		return s.handleUserDraft(ctx, chatID, telegramID, text, draft)
	}
	if draft.Kind == "edit" {
		return s.handleEditDraft(ctx, chatID, telegramID, text, draft)
	}
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

func (s *Service) startUserDraft(ctx context.Context, chatID, telegramID int64) error {
	user, err := s.userForTelegram(ctx, telegramID)
	if err != nil || user == nil || !hasPermission(user.Permissions, "USER_CREATE") {
		return s.telegram.Send(ctx, chatID, "Создавать пользователей могут только администраторы.")
	}
	if err = s.drafts.Save(ctx, telegramID, &Draft{Kind: "user", Step: "login"}); err != nil {
		return err
	}
	return s.telegram.Send(ctx, chatID, "Укажите логин нового пользователя.")
}

func (s *Service) handleUserDraft(ctx context.Context, chatID, telegramID int64, text string, draft *Draft) error {
	if draft.Step == "confirm" {
		if text != "/confirm" {
			return s.telegram.Send(ctx, chatID, "Отправьте /confirm или /cancel.")
		}
		return s.confirmUserDraft(ctx, chatID, telegramID, draft)
	}
	switch draft.Step {
	case "login":
		draft.UserLogin, draft.Step, text = text, "name", "Укажите полное имя."
	case "name":
		draft.UserFullName, draft.Step, text = text, "role", "Укажите роль: admin или performer."
	case "role":
		role := strings.ToLower(text)
		if role != "admin" && role != "performer" {
			return s.telegram.Send(ctx, chatID, "Допустимые роли: admin или performer.")
		}
		draft.UserRole, draft.Step = role, "confirm"
		if err := s.drafts.Save(ctx, telegramID, draft); err != nil {
			return err
		}
		return s.telegram.Send(ctx, chatID, "Создать пользователя "+draft.UserLogin+" с ролью "+role+"? Отправьте /confirm или /cancel.")
	}
	if err := s.drafts.Save(ctx, telegramID, draft); err != nil {
		return err
	}
	return s.telegram.Send(ctx, chatID, text)
}

func (s *Service) confirmUserDraft(ctx context.Context, chatID, telegramID int64, draft *Draft) error {
	actor, err := s.userForTelegram(ctx, telegramID)
	if err != nil || actor == nil || !hasPermission(actor.Permissions, "USER_CREATE") {
		return s.telegram.Send(ctx, chatID, "Недостаточно прав.")
	}
	password, err := temporaryPassword()
	if err != nil {
		return err
	}
	role := userv1.Role_ROLE_PERFORMER
	if draft.UserRole == "admin" {
		role = userv1.Role_ROLE_ADMIN
	}
	_, err = s.users.CreateUser(s.auth(ctx, actor.Id, actor.Permissions), &userv1.CreateUserRequest{Login: draft.UserLogin, FullName: draft.UserFullName, Password: password, Role: role})
	if err != nil {
		slog.Warn("user creation failed", "error", err)
		return s.telegram.Send(ctx, chatID, "Не удалось создать пользователя. Проверьте логин.")
	}
	if err = s.drafts.Delete(ctx, telegramID); err != nil {
		return err
	}
	return s.telegram.Send(ctx, chatID, "Пользователь создан. Временный пароль: "+password+"\nПередайте его пользователю безопасным способом.")
}

func temporaryPassword() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Service) startEditDraft(ctx context.Context, chatID, telegramID int64, orderID string) error {
	user, err := s.userForTelegram(ctx, telegramID)
	if err != nil || user == nil || !hasPermission(user.Permissions, "ORDER_UPDATE") {
		return s.telegram.Send(ctx, chatID, "Редактировать заказы могут только администраторы.")
	}
	response, err := s.orders.GetOrder(s.auth(ctx, user.Id, user.Permissions), &orderv1.GetOrderRequest{Id: orderID})
	if err != nil || response.Order == nil {
		return s.telegram.Send(ctx, chatID, "Заказ не найден.")
	}
	o := response.Order
	draft := &Draft{Kind: "edit", Step: "date", OrderID: o.Id, Version: o.Version, OrderDate: o.OrderDate.AsTime().Format("02.01.2006 15:04"), Location: o.Location, Amount: o.Amount, CustomerName: o.CustomerName, CustomerPhone: o.CustomerPhone, CustomerContact: o.CustomerContact, Comment: o.Comment}
	if err = s.drafts.Save(ctx, telegramID, draft); err != nil {
		return err
	}
	return s.telegram.Send(ctx, chatID, "Текущая дата: "+draft.OrderDate+". Введите новую дату ДД.ММ.ГГГГ ЧЧ:ММ или '-'.")
}

func (s *Service) handleEditDraft(ctx context.Context, chatID, telegramID int64, text string, draft *Draft) error {
	if draft.Step == "confirm" {
		if text != "/confirm" {
			return s.telegram.Send(ctx, chatID, "Отправьте /confirm или /cancel.")
		}
		return s.confirmEditDraft(ctx, chatID, telegramID, draft)
	}
	set := func(current *string) {
		if text != "-" {
			*current = text
		}
	}
	var prompt string
	switch draft.Step {
	case "date":
		if text != "-" {
			if _, err := time.Parse("02.01.2006 15:04", text); err != nil {
				return s.telegram.Send(ctx, chatID, "Неверный формат даты.")
			}
			draft.OrderDate = text
		}
		draft.Step, prompt = "location", "Новый адрес или '-'."
	case "location":
		set(&draft.Location)
		draft.Step, prompt = "amount", "Новая сумма или '-'."
	case "amount":
		set(&draft.Amount)
		draft.Step, prompt = "customer_name", "Новое имя клиента или '-'."
	case "customer_name":
		set(&draft.CustomerName)
		draft.Step, prompt = "customer_phone", "Новый телефон клиента или '-'."
	case "customer_phone":
		set(&draft.CustomerPhone)
		draft.Step, prompt = "customer_contact", "Новый контакт клиента или '-'."
	case "customer_contact":
		set(&draft.CustomerContact)
		draft.Step, prompt = "comment", "Новый комментарий или '-'."
	case "comment":
		set(&draft.Comment)
		draft.Step = "confirm"
		if err := s.drafts.Save(ctx, telegramID, draft); err != nil {
			return err
		}
		return s.telegram.Send(ctx, chatID, draftSummary(draft)+"\n\nОтправьте /confirm или /cancel.")
	}
	if err := s.drafts.Save(ctx, telegramID, draft); err != nil {
		return err
	}
	return s.telegram.Send(ctx, chatID, prompt)
}

func (s *Service) confirmEditDraft(ctx context.Context, chatID, telegramID int64, draft *Draft) error {
	user, err := s.userForTelegram(ctx, telegramID)
	if err != nil || user == nil || !hasPermission(user.Permissions, "ORDER_UPDATE") {
		return s.telegram.Send(ctx, chatID, "Недостаточно прав.")
	}
	date, err := time.Parse("02.01.2006 15:04", draft.OrderDate)
	if err != nil {
		return err
	}
	_, err = s.orders.UpdateOrder(s.auth(ctx, user.Id, user.Permissions), &orderv1.UpdateOrderRequest{Id: draft.OrderID, ExpectedVersion: draft.Version, OrderDate: timestamppb.New(date), Location: draft.Location, Amount: draft.Amount, CustomerName: draft.CustomerName, CustomerPhone: draft.CustomerPhone, CustomerContact: draft.CustomerContact, Comment: draft.Comment})
	if err != nil {
		return s.telegram.Send(ctx, chatID, "Не удалось сохранить заказ: он мог быть изменён другим пользователем.")
	}
	if err = s.drafts.Delete(ctx, telegramID); err != nil {
		return err
	}
	return s.telegram.Send(ctx, chatID, "Заказ обновлён.")
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

func (s *Service) bindWithPassword(ctx context.Context, chatID, telegramID int64, login, password string) error {
	authenticated, err := s.users.Authenticate(ctx, &userv1.AuthenticateRequest{Login: login, Password: password})
	if err != nil || authenticated.User == nil {
		return s.telegram.Send(ctx, chatID, "Неверный логин или пароль.")
	}
	_, err = s.users.BindTelegram(s.auth(ctx, authenticated.User.Id, authenticated.User.Permissions), &userv1.BindTelegramRequest{Id: authenticated.User.Id, TelegramId: telegramID})
	if err != nil {
		slog.Warn("telegram password binding failed", "error", err)
		return s.telegram.Send(ctx, chatID, "Не удалось привязать Telegram-аккаунт. Возможно, он уже используется.")
	}
	return s.telegram.Send(ctx, chatID, "Аккаунт успешно привязан. Используйте /orders для просмотра заказов.")
}

func (s *Service) sendWelcome(ctx context.Context, chatID, telegramID int64) error {
	user, err := s.users.GetUserByTelegramID(s.auth(ctx, "telegram-bot", nil), &userv1.GetUserByTelegramIDRequest{TelegramId: telegramID})
	if err != nil || user.User == nil {
		return s.telegram.Send(ctx, chatID, "Чтобы начать, отправьте /start <логин> <пароль>.")
	}
	return s.telegram.Send(ctx, chatID, "Здравствуйте, "+user.User.FullName+"! Используйте /orders для просмотра активных заказов.")
}

func (s *Service) sendOrders(ctx context.Context, chatID, telegramID int64) error {
	user, err := s.users.GetUserByTelegramID(s.auth(ctx, "telegram-bot", nil), &userv1.GetUserByTelegramIDRequest{TelegramId: telegramID})
	if err != nil || user.User == nil {
		return s.telegram.Send(ctx, chatID, "Сначала привяжите аккаунт: /start <логин> <пароль>.")
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

func (s *Service) sendOrdersForDate(ctx context.Context, chatID, telegramID int64, value string) error {
	day, err := time.Parse("02.01.2006", value)
	if err != nil {
		return s.telegram.Send(ctx, chatID, "Неверный формат даты. Пример: /ordersdate 25.09.2026")
	}
	user, err := s.userForTelegram(ctx, telegramID)
	if err != nil || user == nil || !hasPermission(user.Permissions, "ORDER_READ_ALL") {
		return s.telegram.Send(ctx, chatID, "Просматривать все заказы могут только администраторы.")
	}
	response, err := s.orders.ListOrders(s.auth(ctx, user.Id, user.Permissions), &orderv1.ListOrdersRequest{PageSize: 100})
	if err != nil {
		return s.telegram.Send(ctx, chatID, "Не удалось получить список заказов.")
	}
	lines := []string{"Заказы на " + day.Format("02.01.2006") + ":"}
	for _, order := range response.Orders {
		if order.OrderDate.AsTime().Format("2006-01-02") != day.Format("2006-01-02") {
			continue
		}
		lines = append(lines, fmt.Sprintf("• %s — %s, %s, %s", order.OrderDate.AsTime().Format("15:04"), order.Location, order.Amount, order.CustomerName))
	}
	if len(lines) == 1 {
		return s.telegram.Send(ctx, chatID, "Заказов на эту дату нет.")
	}
	return s.telegram.Send(ctx, chatID, strings.Join(lines, "\n"))
}

func (s *Service) auth(ctx context.Context, subject string, permissions []string) context.Context {
	claims := jwt.MapClaims{"role": "BOT", "permissions": permissions, "sub": subject, "exp": time.Now().Add(5 * time.Minute).Unix()}
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.secret))
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}
