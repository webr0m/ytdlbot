package app

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"webrom/ytdlbot/internal/safeurl"
	"webrom/ytdlbot/internal/telegram"
)

//go:embed static/*
var assets embed.FS

type Server struct {
	Config      Config
	Store       *Store
	Engine      *Engine
	Bot         *telegram.Client
	ramGate     chan struct{}
	inspectGate chan struct{}
	uploadGate  chan struct{}
	wake        chan struct{}
	rateMu      sync.Mutex
	rates       map[int64][]time.Time
	diskMu      sync.Mutex
	diskChecked time.Time
	diskOK      bool
	concurrency atomic.Int32
	activeSlots atomic.Int32
}

func NewServer(c Config, store *Store, engine *Engine) *Server {
	uploads := c.UploadConcurrency
	if uploads < 1 {
		uploads = 2
	}
	s := &Server{
		Config: c, Store: store, Engine: engine,
		ramGate:     make(chan struct{}, 1),
		inspectGate: make(chan struct{}, 2),
		uploadGate:  make(chan struct{}, uploads),
		wake:        make(chan struct{}, 1),
		rates:       map[int64][]time.Time{},
	}
	s.concurrency.Store(int32(c.Workers))
	if c.Token != "" {
		s.Bot = telegram.New(c.TelegramURL, c.Token)
	}
	return s
}

func (s *Server) signalQueue() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// hasDiskFree caches the disk check briefly. Workers used to hit the
// filesystem once per second each; under load this was unnecessary overhead.
func (s *Server) hasDiskFree() bool {
	s.diskMu.Lock()
	defer s.diskMu.Unlock()
	if !s.diskChecked.IsZero() && time.Since(s.diskChecked) < 5*time.Second {
		return s.diskOK
	}
	du, e := disk.Usage(s.Config.DataDir)
	s.diskChecked = time.Now()
	s.diskOK = e == nil && du.Free >= s.Config.MinFreeBytes
	return s.diskOK
}

func (s *Server) uploadSlot(ctx context.Context, send func() (string, error)) (string, error) {
	select {
	case s.uploadGate <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-s.uploadGate }()
	return send()
}

func (s *Server) uploadSlotErr(ctx context.Context, send func() error) error {
	select {
	case s.uploadGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.uploadGate }()
	return send()
}
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, e error) { write(w, 400, map[string]string{"error": e.Error()}) }
func read(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return fmt.Errorf("solicitud inválida")
	}
	var extra any
	if d.Decode(&extra) == nil {
		return fmt.Errorf("solicitud inválida")
	}
	return nil
}
func (s *Server) auth(r *http.Request) (telegram.User, error) {
	if strings.HasPrefix(r.Header.Get("Authorization"), "tma ") {
		return telegram.ValidateInitData(strings.TrimPrefix(r.Header.Get("Authorization"), "tma "), s.Config.Token, time.Now())
	}
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if s.Config.LocalKey != "" && subtle.ConstantTimeCompare([]byte(key), []byte(s.Config.LocalKey)) == 1 {
		// A separate key works only through a direct loopback request. Cloudflare
		// headers cannot turn the localhost reverse-proxy connection into a login.
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() || r.Header.Get("CF-Ray") != "" || r.Header.Get("CF-Connecting-IP") != "" || r.Header.Get("Forwarded") != "" || r.Header.Get("X-Forwarded-For") != "" {
			return telegram.User{}, fmt.Errorf("acceso local únicamente")
		}
		for id := range s.Config.Admins {
			return telegram.User{ID: id, FirstName: "Administrador local"}, nil
		}
		return telegram.User{ID: 1, FirstName: "Vista local"}, nil
	}
	return telegram.User{}, fmt.Errorf("abre la Mini App desde Telegram")
}
func (s *Server) allowRate(user int64) bool {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	now := time.Now()
	if len(s.rates) > 10000 {
		for k, t := range s.rates {
			if len(t) == 0 || now.Sub(t[len(t)-1]) > time.Minute {
				delete(s.rates, k)
			}
		}
	}
	entries := s.rates[user]
	cut := 0
	for cut < len(entries) && now.Sub(entries[cut]) > time.Minute {
		cut++
	}
	entries = entries[cut:]
	if len(entries) >= 120 {
		s.rates[user] = entries
		return false
	}
	s.rates[user] = append(entries, now)
	return true
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if s.Store.Pool.Ping(ctx) != nil {
			write(w, 503, map[string]string{"status": "database unavailable"})
			return
		}
		write(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		user, e := s.auth(r)
		if e != nil {
			write(w, 401, map[string]string{"error": e.Error()})
			return
		}
		if !s.Config.CanUse(user.ID) && !(user.ID == 1 && len(s.Config.Admins) == 0 && s.Config.Token == "") {
			write(w, 403, map[string]string{"error": "tu cuenta no tiene acceso; contacta al administrador"})
			return
		}
		if !s.allowRate(user.ID) {
			write(w, 429, map[string]string{"error": "demasiadas solicitudes; espera un minuto"})
			return
		}
		s.route(w, r, user)
	})
	sub, _ := fs.Sub(assets, "static")
	files := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "método no permitido", 405)
			return
		}
		if _, e := fs.Stat(sub, strings.TrimPrefix(r.URL.Path, "/")); e != nil {
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' https://telegram.org; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors https://web.telegram.org https://*.telegram.org;")
		mux.ServeHTTP(w, r)
	})
}
func (s *Server) route(w http.ResponseWriter, r *http.Request, user telegram.User) {
	ctx := r.Context()
	switch r.Method + " " + r.URL.Path {
	case "GET /api/me":
		a, e := s.Store.Account(ctx, user.ID)
		if e != nil {
			fail(w, fmt.Errorf("no se pudo consultar la cuenta"))
			return
		}
		settings, e := s.Store.Settings(ctx, user.ID)
		if e != nil {
			fail(w, fmt.Errorf("no se pudieron consultar preferencias"))
			return
		}
		write(w, 200, map[string]any{"user": user, "account": a, "settings": settings, "limits": map[string]any{"fileMB": s.Config.MaxBytes / 1000000, "workers": s.concurrency.Load(), "activePerUser": s.Config.MaxActivePerUser, "uploads": s.Config.UploadConcurrency, "playlist": s.Config.PlaylistLimit, "ramPartMB": s.Config.RAMPartMB, "savedUploads": s.canSave(Job{UserID: user.ID, ChatID: user.ID})}, "premiumStars": s.Config.PremiumStars, "paymentsEnabled": s.Config.Payments, "supportUrl": s.Config.SupportURL, "botUsername": s.Config.BotUsername, "deliveryReady": s.Bot != nil})
	case "GET /api/jobs":
		jobs, e := s.Store.List(ctx, user.ID)
		if e != nil {
			fail(w, fmt.Errorf("no se pudo consultar la cola"))
			return
		}
		write(w, 200, jobs)
	case "POST /api/jobs":
		var p struct {
			URL     string  `json:"url"`
			Options Options `json:"options"`
		}
		if e := read(w, r, &p); e != nil {
			fail(w, e)
			return
		}
		if e := safeurl.Validate(ctx, p.URL); e != nil {
			fail(w, e)
			return
		}
		if s.Bot == nil {
			fail(w, fmt.Errorf("configura BOT_TOKEN antes de crear descargas; el panel ya está disponible"))
			return
		}
		j, e := s.Store.Create(ctx, user.ID, user.ID, strings.TrimSpace(p.URL), p.Options)
		if e != nil {
			fail(w, e)
			return
		}
		s.signalQueue()
		write(w, 201, j)
	case "POST /api/inspect":
		var p struct {
			URL string `json:"url"`
		}
		if e := read(w, r, &p); e != nil {
			fail(w, e)
			return
		}
		select {
		case s.inspectGate <- struct{}{}:
			defer func() { <-s.inspectGate }()
		default:
			write(w, 429, map[string]string{"error": "el analizador está ocupado; reintenta en unos segundos"})
			return
		}
		info, e := s.Engine.Inspect(ctx, p.URL)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, info)
	case "PUT /api/settings":
		var o Options
		if e := read(w, r, &o); e != nil {
			fail(w, e)
			return
		}
		if e := s.Store.SaveSettings(ctx, user.ID, o); e != nil {
			fail(w, e)
			return
		}
		write(w, 200, map[string]bool{"ok": true})
	case "POST /api/premium/invoice":
		link, e := s.invoice(ctx, user.ID)
		if e != nil {
			fail(w, e)
			return
		}
		write(w, 200, map[string]string{"url": link})
	case "POST /api/premium/cancel":
		if e := s.cancelSubscription(ctx, user.ID); e != nil {
			fail(w, e)
			return
		}
		write(w, 200, map[string]bool{"ok": true})
	case "GET /api/system":
		if !s.Config.Admins[user.ID] && !(user.ID == 1 && s.Config.Token == "") {
			write(w, 403, map[string]string{"error": "solo administradores"})
			return
		}
		vm, _ := mem.VirtualMemory()
		du, _ := disk.Usage(s.Config.DataDir)
		cp, _ := cpu.Percent(0, false)
		var paused bool
		s.Store.Pool.QueryRow(ctx, "SELECT COALESCE((SELECT value=1 FROM bot_state WHERE key='paused'),false)").Scan(&paused)
		var queued, active int
		s.Store.Pool.QueryRow(ctx, "SELECT count(*) FILTER(WHERE state='queued'),count(*) FILTER(WHERE state IN ('downloading','uploading')) FROM jobs").Scan(&queued, &active)
		engines := map[string]bool{}
		for k, p := range map[string]string{"yt-dlp": s.Config.YTDLP, "gallery-dl": s.Config.GalleryDL, "ffmpeg": s.Config.FFmpeg} {
			_, e := exec.LookPath(p)
			engines[k] = e == nil
		}
		write(w, 200, map[string]any{"memory": vm, "disk": du, "cpu": cp, "paused": paused, "queued": queued, "active": active, "engines": engines, "workers": s.concurrency.Load(), "activePerUser": s.Config.MaxActivePerUser, "uploads": s.Config.UploadConcurrency})
	case "POST /api/system/concurrency":
		if !s.Config.Admins[user.ID] {
			write(w, 403, map[string]string{"error": "solo administradores"})
			return
		}
		var p struct {
			Workers int `json:"workers"`
		}
		if e := read(w, r, &p); e != nil {
			fail(w, e)
			return
		}
		if e := s.setConcurrency(ctx, p.Workers); e != nil {
			fail(w, e)
			return
		}
		write(w, 200, map[string]int{"workers": p.Workers})
	case "POST /api/system/queue":
		if !s.Config.Admins[user.ID] {
			write(w, 403, map[string]string{"error": "solo administradores"})
			return
		}
		var p struct {
			Paused bool `json:"paused"`
		}
		if e := read(w, r, &p); e != nil {
			fail(w, e)
			return
		}
		value := 0
		if p.Paused {
			value = 1
		}
		if _, e := s.Store.Pool.Exec(ctx, "INSERT INTO bot_state(key,value) VALUES('paused',$1) ON CONFLICT(key) DO UPDATE SET value=excluded.value", value); e != nil {
			fail(w, fmt.Errorf("no se pudo cambiar la cola"))
			return
		}
		s.signalQueue()
		write(w, 200, map[string]bool{"ok": true})
	case "GET /api/admin/payments":
		if !s.Config.Admins[user.ID] {
			write(w, 403, map[string]string{"error": "solo administradores"})
			return
		}
		rows, e := s.Store.Pool.Query(ctx, "SELECT charge_id,user_id,stars,expires_at,refunded,cancelled FROM subscriptions ORDER BY paid_at DESC LIMIT 100")
		if e != nil {
			fail(w, fmt.Errorf("no se pudo consultar pagos"))
			return
		}
		defer rows.Close()
		payments := []map[string]any{}
		for rows.Next() {
			var charge string
			var uid int64
			var stars int
			var expires time.Time
			var refunded, cancelled bool
			rows.Scan(&charge, &uid, &stars, &expires, &refunded, &cancelled)
			payments = append(payments, map[string]any{"charge": charge, "userId": uid, "stars": stars, "expires": expires, "refunded": refunded, "cancelled": cancelled})
		}
		write(w, 200, payments)
	case "POST /api/admin/refund":
		if !s.Config.Admins[user.ID] || s.Bot == nil {
			write(w, 403, map[string]string{"error": "solo administradores"})
			return
		}
		var p struct {
			Charge string `json:"charge"`
		}
		if e := read(w, r, &p); e != nil {
			fail(w, e)
			return
		}
		var uid int64
		e := s.Store.Pool.QueryRow(ctx, "SELECT user_id FROM subscriptions WHERE charge_id=$1 AND NOT refunded", p.Charge).Scan(&uid)
		if e != nil {
			fail(w, fmt.Errorf("pago no encontrado"))
			return
		}
		if e = s.Bot.Call(ctx, "refundStarPayment", map[string]any{"user_id": uid, "telegram_payment_charge_id": p.Charge}, nil); e != nil {
			fail(w, e)
			return
		}
		if _, e = s.Store.Pool.Exec(ctx, "UPDATE subscriptions SET refunded=true WHERE charge_id=$1", p.Charge); e != nil {
			fail(w, fmt.Errorf("reembolso enviado; sincronización pendiente del evento de Telegram"))
			return
		}
		write(w, 200, map[string]bool{"ok": true})
	default:
		if strings.HasPrefix(r.URL.Path, "/api/jobs/") && r.Method == "POST" {
			s.jobAction(w, r, user)
			return
		}
		http.NotFound(w, r)
	}
}
func (s *Server) jobAction(w http.ResponseWriter, r *http.Request, user telegram.User) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/jobs/"), "/")
	if len(parts) != 2 || !jobIDPattern.MatchString(parts[0]) {
		http.NotFound(w, r)
		return
	}
	id, action := parts[0], parts[1]
	ctx := r.Context()
	if action == "cancel" {
		if e := s.Store.Cancel(ctx, id, user.ID); e != nil {
			fail(w, e)
			return
		}
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	j, e := s.Store.Get(ctx, id, user.ID)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	if action == "retry" {
		if j.State != "failed" && j.State != "cancelled" {
			fail(w, fmt.Errorf("solo puedes reintentar tareas fallidas o canceladas"))
			return
		}
		if e = safeurl.Validate(ctx, j.URL); e != nil {
			fail(w, e)
			return
		}
		next, e := s.Store.Create(ctx, user.ID, user.ID, j.URL, j.Options)
		if e != nil {
			fail(w, e)
			return
		}
		s.signalQueue()
		write(w, 201, next)
		return
	}
	if action == "resend" {
		if s.Bot == nil {
			fail(w, fmt.Errorf("Telegram no configurado"))
			return
		}
		var p struct {
			Index int `json:"index"`
		}
		if e = read(w, r, &p); e != nil {
			fail(w, e)
			return
		}
		if p.Index < 0 || p.Index >= len(j.Files) || j.Files[p.Index].FileID == "" {
			fail(w, fmt.Errorf("archivo no disponible"))
			return
		}
		kind := j.Files[p.Index].Kind
		if kind != "video" && kind != "audio" {
			kind = "document"
		}
		payload := map[string]any{"chat_id": user.ID, kind: j.Files[p.Index].FileID}
		if kind == "video" {
			payload["supports_streaming"] = true
		}
		if e = s.Bot.Call(ctx, "send"+strings.ToUpper(kind[:1])+kind[1:], payload, nil); e != nil {
			fail(w, e)
			return
		}
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	http.NotFound(w, r)
}
func (s *Server) menu() any {
	if s.Config.PublicURL == "" {
		return nil
	}
	return map[string]any{"inline_keyboard": [][]map[string]any{{{"text": "🌐 Open / Abrir", "web_app": map[string]string{"url": s.Config.PublicURL}}}}}
}
func (s *Server) RunBot(ctx context.Context) {
	if s.Bot == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Error("bot detenido por un error interno", "panic", r)
		}
	}()
	setupCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	s.configureCommands(setupCtx)
	if s.Config.PublicURL != "" {
		s.Bot.Call(setupCtx, "setChatMenuButton", map[string]any{"menu_button": map[string]any{"type": "web_app", "text": "🌐 Open / Abrir", "web_app": map[string]string{"url": s.Config.PublicURL}}}, nil)
	}
	cancel()
	offset := s.Store.Offset(ctx)
	for ctx.Err() == nil {
		pollCtx, done := context.WithTimeout(ctx, 40*time.Second)
		var updates []telegram.Update
		e := s.Bot.Call(pollCtx, "getUpdates", map[string]any{"offset": offset, "timeout": 25, "allowed_updates": []string{"message", "pre_checkout_query"}}, &updates)
		done()
		if e != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("consulta de actualizaciones fallida", "error", e)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			if ctx.Err() != nil {
				return
			}
			// Never leave the offset stuck on a single bad update: a blocked
			// user, a malformed payment or a temporary Telegram error used to
			// freeze all subsequent messages indefinitely.
			if e = s.processUpdate(ctx, u); e != nil {
				slog.Warn("actualización no procesada", "id", u.ID, "error", e)
			}
			offset = u.ID + 1
			if e = s.Store.SetOffset(ctx, offset); e != nil {
				slog.Warn("no se pudo guardar el offset del bot", "error", e)
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
		}
	}
}

func (s *Server) processUpdate(ctx context.Context, u telegram.Update) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("error interno al procesar la actualización")
			slog.Error("panic procesando actualización", "id", u.ID, "panic", r)
		}
	}()
	for attempt := 0; attempt < 3; attempt++ {
		err = s.handleUpdate(ctx, u)
		if err == nil {
			return nil
		}
		var apiErr *telegram.APIError
		if errors.As(err, &apiErr) && apiErr.Code != 429 && apiErr.Code < 500 {
			return err
		}
		if ctx.Err() != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
	return err
}
func (s *Server) handleUpdate(parent context.Context, u telegram.Update) error {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	if p := u.PreCheckout; p != nil {
		e := s.Store.ValidateCheckout(ctx, *p)
		payload := map[string]any{"pre_checkout_query_id": p.ID, "ok": e == nil}
		if e != nil {
			payload["error_message"] = e.Error()
		}
		return s.Bot.Call(ctx, "answerPreCheckoutQuery", payload, nil)
	}
	m := u.Message
	if m == nil {
		return nil
	}
	if m.Payment != nil {
		return s.Store.ApplyPayment(ctx, m.From.ID, *m.Payment)
	}
	if m.Refund != nil {
		_, e := s.Store.Pool.Exec(ctx, "UPDATE subscriptions SET refunded=true WHERE charge_id=$1 AND user_id=$2", m.Refund.ChargeID, m.From.ID)
		return e
	}
	if m.Chat.Type != "private" {
		return nil
	}
	if !s.Config.CanUse(m.From.ID) {
		s.Bot.Message(ctx, m.Chat.ID, "Tu cuenta aún no tiene acceso. Contacta al administrador.", nil)
		return nil
	}
	view := "download"
	command := strings.TrimPrefix(strings.Split(strings.TrimSpace(m.Text), " ")[0], "/")
	switch command {
	case "queue", "history", "premium", "account":
		view = command
	case "settings":
		view = "account"
	case "help", "paysupport", "terms", "privacy":
		view = "help"
	}
	text := "🇬🇧 Your download hub is ready. Open the Mini App to choose quality, track your queue and receive files here.\n\n🇪🇸 Tu centro de descargas está listo. Abre la Mini App para elegir calidad, ver la cola y recibir tus archivos aquí."
	if view == "help" {
		text = "🇪🇸 Ayuda, condiciones y privacidad: abre Ayuda en la Mini App. Pagos y reembolsos: " + s.Config.SupportURL + "\n\n🇬🇧 Help, terms and privacy: open Help in the Mini App. Payments and refunds: " + s.Config.SupportURL
	}
	s.Bot.Message(ctx, m.Chat.ID, text, s.menuFor(m.From, view))
	return nil
}
func localPath(base, id, name string) string {
	return filepath.Join(base, "jobs", id, filepath.Base(name))
}
