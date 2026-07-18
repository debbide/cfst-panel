package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/debbide/cfst-panel/internal/model"
	"github.com/debbide/cfst-panel/internal/service"
	"github.com/debbide/cfst-panel/internal/store"
	"github.com/debbide/cfst-panel/internal/webui"
)

type Server struct {
	app    *service.App
	webDir string
	listen string
	mux    *http.ServeMux
}

func NewServer(app *service.App, webDir, listen string) *Server {
	s := &Server{app: app, webDir: strings.TrimSpace(webDir), listen: listen, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.withCORS(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/login", s.handleLogin)
	s.mux.HandleFunc("/api/logout", s.auth(s.handleLogout))
	s.mux.HandleFunc("/api/status", s.auth(s.handleStatus))
	s.mux.HandleFunc("/api/settings", s.auth(s.handleSettings))
	s.mux.HandleFunc("/api/records", s.auth(s.handleRecords))
	s.mux.HandleFunc("/api/records/", s.auth(s.handleRecordItem))
	s.mux.HandleFunc("/api/tasks", s.auth(s.handleTasksRoot))
	s.mux.HandleFunc("/api/tasks/", s.auth(s.handleTasksSub))
	s.mux.HandleFunc("/api/logs", s.auth(s.handleLogs))
	s.mux.HandleFunc("/api/test/cloudflare", s.auth(s.handleTestCloudflare))
	s.mux.HandleFunc("/api/test/telegram", s.auth(s.handleTestTelegram))

	// Static frontend with SPA fallback.
	s.mux.HandleFunc("/", s.handleFrontend)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": service.Version})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	token, exp, err := s.app.Login(body.Username, body.Password)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "cfst_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  exp,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"token":      token,
		"expires_at": exp,
		"username":   body.Username,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	token := s.tokenFromRequest(r)
	_ = s.app.Logout(token)
	http.SetCookie(w, &http.Cookie{
		Name:     "cfst_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	st, err := s.app.Status()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := s.app.GetSettings()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, settings)
	case http.MethodPut:
		var settings model.Settings
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		saved, err := s.app.SaveSettings(settings)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleRecords(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.app.ListRecords()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if list == nil {
			list = []model.DNSRecord{}
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		var rec model.DNSRecord
		if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		saved, err := s.app.SaveRecord(rec)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleRecordItem(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/records/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var rec model.DNSRecord
		if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		rec.ID = id
		saved, err := s.app.SaveRecord(rec)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
	case http.MethodDelete:
		if err := s.app.DeleteRecord(id); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleTasksRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.app.ListTasks(limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []model.Task{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleTasksSub(w http.ResponseWriter, r *http.Request) {
	sub := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/tasks/"), "/")
	switch sub {
	case "run":
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		task, err := s.app.RunTask(r.Context(), model.TriggerManual)
		if err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, task)
		return
	case "cancel":
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := s.app.CancelRunning(); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	default:
		if sub == "" {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		task, err := s.app.GetTask(sub)
		if err != nil {
			if store.ErrNotFound(err) {
				writeErr(w, http.StatusNotFound, "task not found")
				return
			}
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, task)
	}
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		level := r.URL.Query().Get("level")
		list, err := s.app.ListLogs(limit, level)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if list == nil {
			list = []model.LogEntry{}
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodDelete:
		if err := s.app.ClearLogs(); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleTestCloudflare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result, err := s.app.TestCloudflare(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTestTelegram(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result, err := s.app.TestTelegram(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleFrontend(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}

	// Prefer external web dir only when explicitly provided.
	if s.webDir != "" {
		s.serveExternalFrontend(w, r)
		return
	}
	s.serveEmbeddedFrontend(w, r)
}

func (s *Server) serveExternalFrontend(w http.ResponseWriter, r *http.Request) {
	root := http.Dir(s.webDir)
	upath := path.Clean("/" + r.URL.Path)
	if upath == "/" {
		upath = "/index.html"
	}
	full := filepath.Join(s.webDir, filepath.FromSlash(upath))
	if info, err := os.Stat(full); err == nil && !info.IsDir() {
		http.FileServer(root).ServeHTTP(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.webDir, "index.html"))
}

func (s *Server) serveEmbeddedFrontend(w http.ResponseWriter, r *http.Request) {
	staticFS, err := fs.Sub(webui.FS, "static")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "embedded ui missing")
		return
	}

	upath := path.Clean("/" + r.URL.Path)
	if upath == "/" {
		http.ServeFileFS(w, r, staticFS, "index.html")
		return
	}
	name := strings.TrimPrefix(upath, "/")
	if info, err := fs.Stat(staticFS, name); err != nil || info.IsDir() {
		http.ServeFileFS(w, r, staticFS, "index.html")
		return
	}
	http.ServeFileFS(w, r, staticFS, name)
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := s.tokenFromRequest(r)
		if token == "" {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if _, err := s.app.Auth(token); err != nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

func (s *Server) tokenFromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	if c, err := r.Cookie("cfst_session"); err == nil {
		return c.Value
	}
	return r.URL.Query().Get("token")
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}
