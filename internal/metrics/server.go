package metrics

import (
	"context"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
	"time"
)

var GRPCRequests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "grpc_server_requests_total", Help: "Completed gRPC requests."}, []string{"method", "code"})
var GRPCDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "grpc_server_request_duration_seconds", Help: "gRPC request duration."}, []string{"method"})

func init() { prometheus.MustRegister(GRPCRequests, GRPCDuration) }
func Serve(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	s := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(shutdownCtx)
	}()
	return s.ListenAndServe()
}
