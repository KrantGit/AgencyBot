package bot

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const draftTTL = 30 * time.Minute

type Draft struct {
	Kind, UserLogin, UserFullName, UserRole, OrderID                                         string
	Version                                                                                  int64
	Step, OrderDate, Location, Amount, CustomerName, CustomerPhone, CustomerContact, Comment string
	PerformerIDs                                                                             []string
}

type DraftStore struct{ address string }

func NewDraftStore(address string) *DraftStore { return &DraftStore{address: address} }

func (s *DraftStore) Load(ctx context.Context, telegramID int64) (*Draft, error) {
	value, found, err := s.command(ctx, "GET", draftKey(telegramID))
	if err != nil || !found {
		return nil, err
	}
	var draft Draft
	if err = json.Unmarshal([]byte(value), &draft); err != nil {
		return nil, err
	}
	return &draft, nil
}

func (s *DraftStore) Save(ctx context.Context, telegramID int64, draft *Draft) error {
	data, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	_, _, err = s.command(ctx, "SET", draftKey(telegramID), string(data), "EX", strconv.Itoa(int(draftTTL.Seconds())))
	return err
}

func (s *DraftStore) Delete(ctx context.Context, telegramID int64) error {
	_, _, err := s.command(ctx, "DEL", draftKey(telegramID))
	return err
}

func draftKey(telegramID int64) string { return "bot:order-draft:" + strconv.FormatInt(telegramID, 10) }

func (s *DraftStore) command(ctx context.Context, values ...string) (string, bool, error) {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", s.address)
	if err != nil {
		return "", false, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	}
	var request strings.Builder
	fmt.Fprintf(&request, "*%d\r\n", len(values))
	for _, value := range values {
		fmt.Fprintf(&request, "$%d\r\n%s\r\n", len(value), value)
	}
	if _, err = conn.Write([]byte(request.String())); err != nil {
		return "", false, err
	}
	reader := bufio.NewReader(conn)
	prefix, err := reader.ReadByte()
	if err != nil {
		return "", false, err
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", false, err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	switch prefix {
	case '+', ':':
		return line, true, nil
	case '-':
		return "", false, fmt.Errorf("redis: %s", line)
	case '$':
		size, err := strconv.Atoi(line)
		if err != nil {
			return "", false, err
		}
		if size == -1 {
			return "", false, nil
		}
		data := make([]byte, size+2)
		if _, err = reader.Read(data); err != nil {
			return "", false, err
		}
		return string(data[:size]), true, nil
	default:
		return "", false, fmt.Errorf("unexpected redis response %q", prefix)
	}
}
