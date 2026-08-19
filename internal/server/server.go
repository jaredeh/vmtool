package server

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jaredeh/vmtool/internal/api"
	"github.com/jaredeh/vmtool/internal/app"
)

// Server is the vmtool REST API.
type Server struct {
	Listen      string
	PlaybookDir string
}

// muxHandler serves JSON routes via the generated strict wrapper and overrides
// OpenVMConsole so the WebSocket upgrade can hijack the connection.
type muxHandler struct {
	api.ServerInterface
	h *handlers
}

func (m muxHandler) OpenVMConsole(w http.ResponseWriter, r *http.Request, name api.VMName, params api.OpenVMConsoleParams) {
	m.h.serveConsole(w, r, string(name), params)
}

// Handler returns the generated mux wrapped around StrictServerInterface,
// with OpenVMConsole implemented on ServerInterface.
func (s *Server) Handler() http.Handler {
	if s.PlaybookDir == "" {
		s.PlaybookDir = "ansible/playbooks"
	}
	return handlerFor(&handlers{
		svc:    &app.Service{PlaybookDir: s.PlaybookDir},
		listen: s.Listen,
	})
}

func handlerFor(h *handlers) http.Handler {
	if h.svc == nil {
		h.svc = &app.Service{PlaybookDir: "ansible/playbooks"}
	}
	strict := api.NewStrictHandler(h, nil)
	mux := api.HandlerFromMux(muxHandler{ServerInterface: strict, h: h}, http.NewServeMux())
	return withMaxBytes(slogRequests(withRequest(mux)))
}

// ListenAndServe starts the API server.
func (s *Server) ListenAndServe() error {
	if s.Listen == "" {
		s.Listen = "127.0.0.1:9473"
	}
	if !isLoopback(s.Listen) {
		slog.Warn("vmtool server is not bound to loopback", "listen", s.Listen)
	}
	srv := &http.Server{
		Addr:              s.Listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	if host == "" || host == "0.0.0.0" || host == "::" {
		return false
	}
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	return false
}

func withMaxBytes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("websocket hijack not supported")
	}
	w.code = http.StatusSwitchingProtocols
	return h.Hijack()
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func withRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestKey, r)))
	})
}

func slogRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: 200}
		next.ServeHTTP(sw, r)
		slog.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.code,
			"duration", time.Since(start),
		)
	})
}
