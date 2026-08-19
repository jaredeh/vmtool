package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"github.com/jaredeh/vmtool/internal/api"
	"github.com/jaredeh/vmtool/internal/app"
	"github.com/jaredeh/vmtool/pkg/vmtool"
)

var (
	_ api.StrictServerInterface = (*handlers)(nil)
	_ api.ServerInterface       = muxHandler{}
)

var errConsoleNotStrict = errors.New("openVMConsole must be served as a WebSocket upgrade")

var upgrader = websocket.Upgrader{
	CheckOrigin: originAllowed,
}

// OpenVMConsole satisfies StrictServerInterface so handlers compile. The mux
// overrides ServerInterface.OpenVMConsole; this path is not used in production.
func (h *handlers) OpenVMConsole(_ context.Context, _ api.OpenVMConsoleRequestObject) (api.OpenVMConsoleResponseObject, error) {
	return nil, errConsoleNotStrict
}

func (h *handlers) serveConsole(w http.ResponseWriter, r *http.Request, name string, params api.OpenVMConsoleParams) {
	if !originAllowed(r) {
		writeJSONError(w, http.StatusForbidden, fmt.Errorf("origin not allowed"))
		return
	}
	if !websocket.IsWebSocketUpgrade(r) {
		writeJSONError(w, http.StatusBadRequest, fmt.Errorf("websocket upgrade required"))
		return
	}
	cmd, err := h.lookupConsoleCmd(name)
	if err != nil {
		writeJSONError(w, consoleStatus(err), err)
		return
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	ptmx, err := pty.StartWithSize(cmd, ptySize(params))
	if err != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		writeJSONError(w, http.StatusBadGateway, err)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		return
	}
	pipeConsole(conn, ptmx, cmd)
}

func (h *handlers) lookupConsoleCmd(name string) (*exec.Cmd, error) {
	if h.consoleCmd != nil {
		return h.consoleCmd(name)
	}
	return defaultConsoleCmd(name)
}

func defaultConsoleCmd(name string) (*exec.Cmd, error) {
	m, err := vmtool.NewManager()
	if err != nil {
		return nil, err
	}
	defer m.Close()
	info, err := m.Info(name)
	if err != nil {
		return nil, err
	}
	if info.IP == "" {
		return nil, &app.Error{Kind: app.KindConflict, Op: "console", Err: fmt.Errorf("VM %q has no IP address", name)}
	}
	c, err := vmtool.SSHCmd(info.IP, info.Name)
	if err != nil {
		return nil, &app.Error{Kind: app.KindBadGateway, Op: "console", Err: err}
	}
	c.Stdin, c.Stdout, c.Stderr = nil, nil, nil
	return c, nil
}

func pipeConsole(conn *websocket.Conn, ptyFile *os.File, cmd *exec.Cmd) {
	defer conn.Close()
	defer func() {
		_ = ptyFile.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()

	var once sync.Once
	done := make(chan struct{})
	stop := func() { once.Do(func() { close(done) }) }

	go func() {
		defer stop()
		buf := make([]byte, 32*1024)
		for {
			n, err := ptyFile.Read(buf)
			if n > 0 {
				if werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	go func() {
		defer stop()
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			switch mt {
			case websocket.TextMessage:
				if cols, rows, ok := parseResize(data); ok {
					_ = pty.Setsize(ptyFile, &pty.Winsize{Cols: cols, Rows: rows})
					continue
				}
				fallthrough
			case websocket.BinaryMessage:
				if _, err := ptyFile.Write(data); err != nil {
					return
				}
			}
		}
	}()

	<-done
}

func ptySize(params api.OpenVMConsoleParams) *pty.Winsize {
	cols, rows := uint16(80), uint16(24)
	if params.Cols != nil {
		cols = clampUint16(*params.Cols, cols)
	}
	if params.Rows != nil {
		rows = clampUint16(*params.Rows, rows)
	}
	return &pty.Winsize{Cols: cols, Rows: rows}
}

func clampUint16(n int, def uint16) uint16 {
	if n < 1 {
		return def
	}
	if n > 65535 {
		return 65535
	}
	return uint16(n)
}

type resizeFrame struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

func parseResize(data []byte) (cols, rows uint16, ok bool) {
	var f resizeFrame
	if json.Unmarshal(data, &f) != nil || f.Type != "resize" || f.Cols == 0 || f.Rows == 0 {
		return 0, 0, false
	}
	return f.Cols, f.Rows, true
}

func originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Host == r.Host {
		return true
	}
	oh := u.Hostname()
	return oh == "localhost" || oh == "127.0.0.1" || oh == "::1"
}

func writeJSONError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiErr(err))
}

func consoleStatus(err error) int {
	if vmtool.IsNotFound(err) {
		return http.StatusNotFound
	}
	var ae *app.Error
	if app.AsError(err, &ae) {
		switch ae.Kind {
		case app.KindNotFound:
			return http.StatusNotFound
		case app.KindConflict:
			return http.StatusConflict
		case app.KindBadGateway:
			return http.StatusBadGateway
		case app.KindInvalid:
			return http.StatusBadRequest
		}
	}
	return http.StatusInternalServerError
}
