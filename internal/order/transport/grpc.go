package transport

import (
	"context"
	"errors"
	orderv1 "github.com/example/order-platform/gen/go/order/v1"
	"github.com/example/order-platform/internal/auth"
	"github.com/example/order-platform/internal/metrics"
	"github.com/example/order-platform/internal/order/app"
	"github.com/example/order-platform/internal/order/domain"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

type Server struct {
	orderv1.UnimplementedOrderServiceServer
	app *app.Service
}

func New(a *app.Service) *Server { return &Server{app: a} }
func (s *Server) CreateOrder(c context.Context, r *orderv1.CreateOrderRequest) (*orderv1.OrderResponse, error) {
	if e := require(c, "ORDER_CREATE"); e != nil {
		return nil, e
	}
	a, e := actor(c)
	if e != nil {
		return nil, e
	}
	ids := []uuid.UUID{}
	for _, x := range r.PerformerIds {
		id, x := uuid.Parse(x)
		if x != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid performer id")
		}
		ids = append(ids, id)
	}
	o := domain.Order{ID: uuid.New(), OrderDate: r.OrderDate.AsTime(), Location: r.Location, Amount: r.Amount, CustomerName: r.CustomerName, CustomerPhone: r.CustomerPhone, CustomerContact: r.CustomerContact, Comment: r.Comment, CreatedBy: a, PerformerIDs: ids}
	e = s.app.Create(c, o)
	return &orderv1.OrderResponse{Order: toProto(o)}, err(e)
}
func (s *Server) GetOrder(c context.Context, r *orderv1.GetOrderRequest) (*orderv1.OrderResponse, error) {
	a, e := actor(c)
	if e != nil {
		return nil, e
	}
	id, x := uuid.Parse(r.Id)
	if x != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid id")
	}
	o, x := s.app.Get(c, id, a, has(c, "ORDER_READ_ALL"))
	return &orderv1.OrderResponse{Order: toProto(o)}, err(x)
}
func (s *Server) ChangeOrderStatus(c context.Context, r *orderv1.ChangeOrderStatusRequest) (*orderv1.OrderResponse, error) {
	if e := require(c, "ORDER_CHANGE_STATUS"); e != nil {
		return nil, e
	}
	a, e := actor(c)
	if e != nil {
		return nil, e
	}
	id, x := uuid.Parse(r.Id)
	if x != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid id")
	}
	m := map[orderv1.Status]domain.Status{orderv1.Status_STATUS_COMPLETED: domain.Completed, orderv1.Status_STATUS_CANCELLED: domain.Cancelled}
	o, x := s.app.ChangeStatus(c, id, a, r.ExpectedVersion, m[r.Status])
	return &orderv1.OrderResponse{Order: toProto(o)}, err(x)
}
func (s *Server) AssignPerformer(c context.Context, r *orderv1.PerformerRequest) (*orderv1.OrderResponse, error) {
	return s.assign(c, r, true)
}
func (s *Server) UnassignPerformer(c context.Context, r *orderv1.PerformerRequest) (*orderv1.OrderResponse, error) {
	return s.assign(c, r, false)
}
func (s *Server) assign(c context.Context, r *orderv1.PerformerRequest, add bool) (*orderv1.OrderResponse, error) {
	if e := require(c, "ORDER_UPDATE"); e != nil {
		return nil, e
	}
	a, e := actor(c)
	if e != nil {
		return nil, e
	}
	oid, e := uuid.Parse(r.OrderId)
	if e != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid order id")
	}
	pid, e := uuid.Parse(r.PerformerId)
	if e != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid performer id")
	}
	o, e := s.app.Assign(c, oid, a, pid, add)
	return &orderv1.OrderResponse{Order: toProto(o)}, err(e)
}
func (s *Server) UpdateOrder(c context.Context, r *orderv1.UpdateOrderRequest) (*orderv1.OrderResponse, error) {
	if e := require(c, "ORDER_UPDATE"); e != nil {
		return nil, e
	}
	a, e := actor(c)
	if e != nil {
		return nil, e
	}
	id, e := uuid.Parse(r.Id)
	if e != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid id")
	}
	o := domain.Order{ID: id, Version: r.ExpectedVersion, UpdatedBy: a, OrderDate: r.OrderDate.AsTime(), Location: r.Location, Amount: r.Amount, CustomerName: r.CustomerName, CustomerPhone: r.CustomerPhone, CustomerContact: r.CustomerContact, Comment: r.Comment}
	o, e = s.app.Update(c, o)
	return &orderv1.OrderResponse{Order: toProto(o)}, err(e)
}
func (s *Server) ListOrders(c context.Context, r *orderv1.ListOrdersRequest) (*orderv1.ListOrdersResponse, error) {
	if !has(c, "ORDER_READ_ALL") {
		return nil, status.Error(codes.PermissionDenied, "ORDER_READ_ALL is required")
	}
	orders, e := s.app.List(c, nil, int(r.PageSize), r.PageToken)
	out := &orderv1.ListOrdersResponse{}
	for _, o := range orders {
		out.Orders = append(out.Orders, toProto(o))
	}
	if len(orders) > 0 {
		out.NextPageToken = orders[len(orders)-1].ID.String()
	}
	return out, err(e)
}
func (s *Server) ListPerformerOrders(c context.Context, r *orderv1.ListPerformerOrdersRequest) (*orderv1.ListOrdersResponse, error) {
	a, e := actor(c)
	if e != nil {
		return nil, e
	}
	id := a
	if r.PerformerId != "" {
		id, e = uuid.Parse(r.PerformerId)
		if e != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid performer id")
		}
		if id != a && !has(c, "ORDER_READ_ALL") {
			return nil, status.Error(codes.PermissionDenied, "forbidden")
		}
	}
	orders, e := s.app.List(c, &id, int(r.PageSize), r.PageToken)
	out := &orderv1.ListOrdersResponse{}
	for _, o := range orders {
		if o.Status == domain.Planned && !o.OrderDate.Before(time.Now()) {
			out.Orders = append(out.Orders, toProto(o))
		}
	}
	return out, err(e)
}
func (s *Server) GetOrderHistory(c context.Context, r *orderv1.GetOrderHistoryRequest) (*orderv1.OrderHistoryResponse, error) {
	if !has(c, "ORDER_HISTORY_READ") {
		return nil, status.Error(codes.PermissionDenied, "ORDER_HISTORY_READ is required")
	}
	id, e := uuid.Parse(r.OrderId)
	if e != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid id")
	}
	items, e := s.app.History(c, id)
	out := &orderv1.OrderHistoryResponse{}
	for _, h := range items {
		p := &orderv1.HistoryItem{Id: h.ID.String(), OrderId: h.OrderID.String(), ActorUserId: h.ActorID.String(), Action: h.Action, CreatedAt: timestamppb.New(h.CreatedAt)}
		for field, v := range h.Changes {
			p.Changes = append(p.Changes, &orderv1.OrderChange{Field: field, OldValue: v.Old, NewValue: v.New})
		}
		out.Items = append(out.Items, p)
	}
	return out, err(e)
}
func actor(c context.Context) (uuid.UUID, error) {
	a, ok := auth.ActorFromContext(c)
	if !ok {
		return uuid.Nil, status.Error(codes.Unauthenticated, "actor absent")
	}
	id, e := uuid.Parse(a.UserID)
	if e != nil {
		return id, status.Error(codes.Unauthenticated, "invalid actor")
	}
	return id, nil
}
func has(c context.Context, p string) bool {
	a, _ := auth.ActorFromContext(c)
	for _, x := range a.Permissions {
		if x == p {
			return true
		}
	}
	return false
}
func require(c context.Context, permission string) error {
	if !has(c, permission) {
		return status.Error(codes.PermissionDenied, permission+" is required")
	}
	return nil
}
func toProto(o domain.Order) *orderv1.Order {
	if o.ID == uuid.Nil {
		return nil
	}
	st := orderv1.Status_STATUS_PLANNED
	if o.Status == domain.Completed {
		st = orderv1.Status_STATUS_COMPLETED
	}
	if o.Status == domain.Cancelled {
		st = orderv1.Status_STATUS_CANCELLED
	}
	ids := []string{}
	for _, x := range o.PerformerIDs {
		ids = append(ids, x.String())
	}
	return &orderv1.Order{Id: o.ID.String(), OrderDate: timestamppb.New(o.OrderDate), Location: o.Location, Amount: o.Amount, CustomerName: o.CustomerName, CustomerPhone: o.CustomerPhone, CustomerContact: o.CustomerContact, Comment: o.Comment, Status: st, CreatedBy: o.CreatedBy.String(), UpdatedBy: o.UpdatedBy.String(), Version: o.Version, PerformerIds: ids, CreatedAt: timestamppb.New(o.CreatedAt), UpdatedAt: timestamppb.New(o.UpdatedAt)}
}
func err(e error) error {
	if e == nil {
		return nil
	}
	if errors.Is(e, domain.ErrNotFound) {
		return status.Error(codes.NotFound, e.Error())
	}
	if errors.Is(e, domain.ErrForbidden) {
		return status.Error(codes.PermissionDenied, e.Error())
	}
	if errors.Is(e, domain.ErrValidation) {
		return status.Error(codes.InvalidArgument, e.Error())
	}
	if errors.Is(e, domain.ErrConflict) {
		return status.Error(codes.Aborted, e.Error())
	}
	return status.Error(codes.Internal, "internal error")
}

func UnaryMetricsInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		started := time.Now()
		response, callErr := handler(ctx, req)
		metrics.GRPCDuration.WithLabelValues(info.FullMethod).Observe(time.Since(started).Seconds())
		metrics.GRPCRequests.WithLabelValues(info.FullMethod, status.Code(callErr).String()).Inc()
		return response, callErr
	}
}

var _ = time.Now
