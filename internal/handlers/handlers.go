package handlers

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"newsanalyzer/internal/auth"
	"newsanalyzer/internal/models"
	"newsanalyzer/internal/processor"
	"newsanalyzer/internal/repo"
	"newsanalyzer/internal/scheduler"
)

//go:embed web/*
var webFS embed.FS

type Handlers struct {
	Repo      *repo.Repo
	Auth      *auth.Auth
	Sched     *scheduler.Scheduler
	Processor *processor.Processor
	StorageFS http.Handler
}

func New(r *repo.Repo, a *auth.Auth, s *scheduler.Scheduler, p *processor.Processor, imagesDir string) *Handlers {
	return &Handlers{
		Repo: r, Auth: a, Sched: s, Processor: p,
		StorageFS: http.StripPrefix("/images/", http.FileServer(http.Dir(imagesDir))),
	}
}

func (h *Handlers) Mux() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/auth/login", h.login)
	mux.HandleFunc("POST /api/auth/refresh", h.refresh)
	mux.HandleFunc("POST /api/auth/logout", h.logout)

	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/me", h.me)

	protected.HandleFunc("GET /api/digests", h.listDigests)
	protected.HandleFunc("POST /api/digests", h.createDigest)
	protected.HandleFunc("GET /api/digests/{id}", h.getDigest)
	protected.HandleFunc("PUT /api/digests/{id}", h.updateDigest)
	protected.HandleFunc("DELETE /api/digests/{id}", h.deleteDigest)
	protected.HandleFunc("POST /api/digests/{id}/run", h.triggerDigest)

	protected.HandleFunc("GET /api/runs", h.listRuns)
	protected.HandleFunc("GET /api/runs/{id}", h.getRun)
	protected.HandleFunc("GET /api/runs/{id}/view-link", h.runViewLink)

	protected.HandleFunc("GET /api/settings", h.getSettings)
	protected.HandleFunc("PUT /api/settings", h.updateSettings)

	protected.HandleFunc("GET /api/users", h.listUsers)
	protected.HandleFunc("POST /api/users", h.createUser)
	protected.HandleFunc("PUT /api/users/{id}", h.updateUser)
	protected.HandleFunc("DELETE /api/users/{id}", h.deleteUser)

	mux.Handle("/api/", h.Auth.Middleware(protected))

	mux.HandleFunc("GET /runs/{id}/view", h.viewRun)
	mux.Handle("/images/", h.StorageFS)

	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(sub)))

	return mux
}

// -------- helpers --------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// normalizeDigestKind validates and normalizes the digest kind.
// Returns the canonical value and true if valid; empty string and false otherwise.
// An empty input is treated as "news" for backward compatibility with old clients.
func normalizeDigestKind(k string) (string, bool) {
	switch k {
	case "":
		return "news", true
	case "news", "facts":
		return k, true
	default:
		return "", false
	}
}

// -------- auth --------

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handlers) login(w http.ResponseWriter, r *http.Request) {
	var in loginReq
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	u, err := h.Repo.GetUserByEmail(r.Context(), strings.ToLower(strings.TrimSpace(in.Email)))
	if err != nil || !auth.CheckPassword(u.PasswordHash, in.Password) {
		writeErr(w, 401, "invalid credentials")
		return
	}
	access, _ := h.Auth.SignAccess(u.ID)
	refresh, err := h.Auth.NewRefresh(r.Context(), u.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"access": access, "refresh": refresh, "user": u})
}

type refreshReq struct {
	Refresh string `json:"refresh"`
}

func (h *Handlers) refresh(w http.ResponseWriter, r *http.Request) {
	var in refreshReq
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	access, newRefresh, _, err := h.Auth.UseRefresh(r.Context(), in.Refresh)
	if err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"access": access, "refresh": newRefresh})
}

func (h *Handlers) logout(w http.ResponseWriter, r *http.Request) {
	var in refreshReq
	_ = decode(r, &in)
	if in.Refresh != "" {
		_ = h.Repo.DeleteRefresh(r.Context(), auth.HashToken(in.Refresh))
	}
	w.WriteHeader(204)
}

func (h *Handlers) me(w http.ResponseWriter, r *http.Request) {
	u, err := h.Repo.GetUserByID(r.Context(), auth.UserID(r))
	if err != nil {
		writeErr(w, 404, "not found")
		return
	}
	writeJSON(w, 200, u)
}

// -------- digests --------

func (h *Handlers) listDigests(w http.ResponseWriter, r *http.Request) {
	list, err := h.Repo.ListDigests(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (h *Handlers) createDigest(w http.ResponseWriter, r *http.Request) {
	var d models.Digest
	if err := decode(r, &d); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	if d.FrequencyHours <= 0 {
		d.FrequencyHours = 24
	}
	if d.Language == "" {
		d.Language = "ru"
	}
	kind, ok := normalizeDigestKind(d.Kind)
	if !ok {
		writeErr(w, 400, "invalid kind")
		return
	}
	d.Kind = kind
	created, err := h.Repo.CreateDigest(r.Context(), d)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if created.Enabled && !h.Sched.Trigger(created.ID) {
		log.Printf("create digest %s: trigger queue full, scheduler tick will pick it up", created.ID)
	}
	writeJSON(w, 200, created)
}

func (h *Handlers) getDigest(w http.ResponseWriter, r *http.Request) {
	d, err := h.Repo.GetDigest(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "not found")
		return
	}
	writeJSON(w, 200, d)
}

func (h *Handlers) updateDigest(w http.ResponseWriter, r *http.Request) {
	var d models.Digest
	if err := decode(r, &d); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	d.ID = r.PathValue("id")
	if d.FrequencyHours <= 0 {
		d.FrequencyHours = 24
	}
	kind, ok := normalizeDigestKind(d.Kind)
	if !ok {
		writeErr(w, 400, "invalid kind")
		return
	}
	d.Kind = kind
	updated, err := h.Repo.UpdateDigest(r.Context(), d)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, updated)
}

func (h *Handlers) deleteDigest(w http.ResponseWriter, r *http.Request) {
	if err := h.Repo.DeleteDigest(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

func (h *Handlers) triggerDigest(w http.ResponseWriter, r *http.Request) {
	if !h.Sched.Trigger(r.PathValue("id")) {
		writeErr(w, 503, "очередь запусков переполнена, попробуйте позже")
		return
	}
	writeJSON(w, 202, map[string]string{"status": "queued"})
}

// -------- runs --------

func (h *Handlers) listRuns(w http.ResponseWriter, r *http.Request) {
	list, err := h.Repo.ListRuns(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (h *Handlers) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := h.Repo.GetRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "not found")
		return
	}
	writeJSON(w, 200, run)
}

func (h *Handlers) viewRun(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	id := r.PathValue("id")
	q := r.URL.Query()
	if !verifyViewLink(h.Auth.Secret, id, q.Get("exp"), q.Get("sig"), time.Now()) {
		http.Error(w, "not found", 404)
		return
	}
	run, err := h.Repo.GetRun(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(run.HTML))
}

// -------- settings --------

func (h *Handlers) getSettings(w http.ResponseWriter, r *http.Request) {
	s, err := h.Repo.GetSettings(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// не отдаём пароли в открытом виде — но отдаём маскированно, чтобы фронт понимал, что значение есть.
	masked := s
	if s.SMTPPassword != "" {
		masked.SMTPPassword = "********"
	}
	if s.CursorAPIKey != "" {
		masked.CursorAPIKey = "********"
	}
	writeJSON(w, 200, masked)
}

func (h *Handlers) updateSettings(w http.ResponseWriter, r *http.Request) {
	current, err := h.Repo.GetSettings(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var s models.Settings
	if err := decode(r, &s); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	if s.CursorAPIKey == "" || s.CursorAPIKey == "********" {
		s.CursorAPIKey = current.CursorAPIKey
	}
	if s.SMTPPassword == "" || s.SMTPPassword == "********" {
		s.SMTPPassword = current.SMTPPassword
	}
	if s.KeepRunsDays < 0 {
		writeErr(w, 400, "keep_runs_days must be >= 0")
		return
	}
	if s.SMTPPort == 0 {
		s.SMTPPort = 587
	}
	if err := h.Repo.UpdateSettings(r.Context(), s); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

// -------- users --------

type userReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handlers) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.Repo.ListUsers(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, users)
}

func (h *Handlers) createUser(w http.ResponseWriter, r *http.Request) {
	var in userReq
	if err := decode(r, &in); err != nil || in.Email == "" || in.Password == "" {
		writeErr(w, 400, "email/password required")
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	u, err := h.Repo.CreateUser(r.Context(), strings.ToLower(strings.TrimSpace(in.Email)), hash)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, u)
}

func (h *Handlers) updateUser(w http.ResponseWriter, r *http.Request) {
	var in userReq
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	id := r.PathValue("id")
	hash := ""
	if in.Password != "" {
		h2, err := auth.HashPassword(in.Password)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		hash = h2
	}
	if err := h.Repo.UpdateUser(r.Context(), id, strings.ToLower(strings.TrimSpace(in.Email)), hash); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if hash != "" {
		_ = h.Repo.DeleteUserRefresh(r.Context(), id)
	}
	w.WriteHeader(204)
}

func (h *Handlers) deleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == auth.UserID(r) {
		writeErr(w, 400, "cannot delete self")
		return
	}
	users, _ := h.Repo.ListUsers(r.Context())
	if len(users) <= 1 {
		writeErr(w, 400, "cannot delete last user")
		return
	}
	if err := h.Repo.DeleteUser(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

