package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jaredeh/vmtool/internal/app"
	"github.com/jaredeh/vmtool/pkg/vmtool"
)

func TestOpenVMConsoleHTTPErrors(t *testing.T) {
	h := &handlers{
		consoleCmd: func(name string) (*exec.Cmd, error) {
			switch name {
			case "missing":
				return nil, fmt.Errorf("looking up domain %q: %w", name, vmtool.ErrNotFound)
			case "noip":
				return nil, &app.Error{Kind: app.KindConflict, Op: "console", Err: fmt.Errorf("VM %q has no IP address", name)}
			case "ssherr":
				return nil, &app.Error{Kind: app.KindBadGateway, Op: "console", Err: fmt.Errorf("ssh failed")}
			case "ok":
				return exec.Command("sleep", "60"), nil
			default:
				return nil, vmtool.ErrNotFound
			}
		},
	}
	ts := httptest.NewServer(handlerFor(h))
	t.Cleanup(ts.Close)

	t.Run("not upgrade", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/vms/ok/console")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("status %d", res.StatusCode)
		}
		if got := errorBody(t, res); !strings.Contains(got, "websocket upgrade required") {
			t.Fatalf("body %q", got)
		}
	})

	t.Run("bad origin", func(t *testing.T) {
		hdr := http.Header{}
		hdr.Set("Origin", "https://evil.example")
		_, res, err := websocket.DefaultDialer.Dial(wsURL(ts, "/vms/ok/console"), hdr)
		if err == nil {
			t.Fatal("expected origin rejection")
		}
		if res == nil {
			t.Fatalf("dial: %v", err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("status %d", res.StatusCode)
		}
	})

	t.Run("missing", func(t *testing.T) {
		assertWSHTTP(t, ts, "/vms/missing/console", http.StatusNotFound)
	})
	t.Run("no ip", func(t *testing.T) {
		assertWSHTTP(t, ts, "/vms/noip/console", http.StatusConflict)
	})
	t.Run("ssh fail", func(t *testing.T) {
		assertWSHTTP(t, ts, "/vms/ssherr/console", http.StatusBadGateway)
	})
}

func TestOpenVMConsolePTY(t *testing.T) {
	h := &handlers{
		consoleCmd: func(name string) (*exec.Cmd, error) {
			if name != "ok" {
				return nil, vmtool.ErrNotFound
			}
			return exec.Command("sleep", "60"), nil
		},
	}
	ts := httptest.NewServer(handlerFor(h))
	t.Cleanup(ts.Close)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(ts, "/vms/ok/console?cols=80&rows=24"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"resize","cols":100,"rows":30}`)); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("ping\n")); err != nil {
		t.Fatal(err)
	}

	var buf []byte
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v (got %q)", err, buf)
		}
		buf = append(buf, msg...)
		if bytes.Contains(buf, []byte("ping")) {
			return
		}
	}
	t.Fatalf("did not see echoed input, got %q", buf)
}

func TestParseResize(t *testing.T) {
	cols, rows, ok := parseResize([]byte(`{"type":"resize","cols":120,"rows":40}`))
	if !ok || cols != 120 || rows != 40 {
		t.Fatalf("got %d %d %v", cols, rows, ok)
	}
	if _, _, ok := parseResize([]byte(`{"type":"resize","cols":0,"rows":24}`)); ok {
		t.Fatal("zero cols")
	}
	if _, _, ok := parseResize([]byte(`{"event":"resize","cols":80,"rows":24}`)); ok {
		t.Fatal("event field is not type")
	}
	if _, _, ok := parseResize([]byte("ping")); ok {
		t.Fatal("plain text")
	}
}

func TestOriginAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9473/vms/web/console", nil)
	if !originAllowed(req) {
		t.Fatal("empty origin")
	}
	req.Header.Set("Origin", "http://127.0.0.1:9473")
	req.Host = "127.0.0.1:9473"
	if !originAllowed(req) {
		t.Fatal("same host")
	}
	req.Header.Set("Origin", "http://localhost:5173")
	if !originAllowed(req) {
		t.Fatal("loopback hostname")
	}
	req.Header.Set("Origin", "https://evil.example")
	if originAllowed(req) {
		t.Fatal("cross-site")
	}
}

func assertWSHTTP(t *testing.T, ts *httptest.Server, path string, want int) {
	t.Helper()
	_, res, err := websocket.DefaultDialer.Dial(wsURL(ts, path), nil)
	if err == nil {
		t.Fatal("expected dial error")
	}
	if res == nil {
		t.Fatalf("dial: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != want {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d want %d body %s", res.StatusCode, want, body)
	}
}

func wsURL(ts *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http") + path
}

func errorBody(t *testing.T, res *http.Response) string {
	t.Helper()
	var e struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&e); err != nil {
		t.Fatal(err)
	}
	return e.Error
}
