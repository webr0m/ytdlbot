package app

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"webrom/ytdlbot/internal/telegram"
)

type Account struct {
	Premium      bool       `json:"premium"`
	Admin        bool       `json:"admin"`
	Expires      *time.Time `json:"expires,omitempty"`
	Cancelled    bool       `json:"cancelled"`
	YouTubeUsed  int        `json:"youtubeUsed"`
	YouTubeLimit int        `json:"youtubeLimit"`
	SocialUsed   int        `json:"socialUsed"`
	SocialLimit  int        `json:"socialLimit"`
	Reset        time.Time  `json:"reset"`
	Period       string     `json:"period"`
}

func category(raw string) string {
	u, _ := url.Parse(raw)
	if u != nil {
		h := strings.ToLower(u.Hostname())
		if h == "youtu.be" || h == "youtube.com" || strings.HasSuffix(h, ".youtube.com") {
			return "youtube"
		}
	}
	return "social"
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Store) account(ctx context.Context, q queryer, user int64) (Account, error) {
	a := Account{Admin: s.Config.Admins[user], YouTubeLimit: 5, SocialLimit: s.Config.DailyLimit, Period: "hoy"}
	if !a.Admin && q != nil {
		var dbAdmin bool
		if e := q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM admins WHERE user_id=$1)", user).Scan(&dbAdmin); e == nil && dbAdmin {
			a.Admin = true
		}
	}
	now := time.Now().UTC()
	since := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	a.Reset = since.Add(24 * time.Hour)
	var expiration time.Time
	var cancelled bool
	e := q.QueryRow(ctx, "SELECT expires_at,cancelled FROM subscriptions WHERE user_id=$1 AND expires_at>now() AND NOT refunded ORDER BY expires_at DESC LIMIT 1", user).Scan(&expiration, &cancelled)
	if e != nil && e != pgx.ErrNoRows {
		return a, e
	}
	if e == nil {
		a.Premium = true
		a.Expires = &expiration
		a.Cancelled = cancelled
		a.YouTubeLimit = 100
		a.SocialLimit = 300
		a.Period = "este período"
		since = expiration.Add(-30 * 24 * time.Hour)
		a.Reset = expiration
	}
	e = q.QueryRow(ctx, "SELECT count(*) FILTER(WHERE category='youtube'),count(*) FILTER(WHERE category='social') FROM jobs WHERE user_id=$1 AND created_at>=$2 AND state NOT IN ('failed','cancelled')", user, since).Scan(&a.YouTubeUsed, &a.SocialUsed)
	return a, e
}
func (s *Store) Account(ctx context.Context, user int64) (Account, error) {
	return s.account(ctx, s.Pool, user)
}
func (s *Store) checkQuota(ctx context.Context, tx pgx.Tx, user int64, raw string, o Options) error {
	a, e := s.account(ctx, tx, user)
	if e != nil {
		return e
	}
	if a.Admin {
		return nil
	}
	if !a.Premium && (o.Split || o.TranscodeRAM || o.Quality > 1080 || o.Playlist || o.Subtitles || o.Start > 0 || o.End > 0 || o.AudioFormat == "flac") {
		return fmt.Errorf("esta opción requiere Premium; puedes usar vídeo hasta 1080p o MP3 gratis")
	}
	if category(raw) == "youtube" && a.YouTubeUsed >= a.YouTubeLimit {
		return fmt.Errorf("límite de YouTube alcanzado (%d %s)", a.YouTubeLimit, a.Period)
	}
	if category(raw) == "social" && a.SocialUsed >= a.SocialLimit {
		return fmt.Errorf("límite de redes/archivos alcanzado (%d %s)", a.SocialLimit, a.Period)
	}
	return nil
}
func (s *Server) invoice(ctx context.Context, user int64) (string, error) {
	if !s.Config.Payments || s.Bot == nil {
		return "", fmt.Errorf("los pagos aún no están habilitados")
	}
	a, e := s.Store.Account(ctx, user)
	if e != nil {
		return "", e
	}
	if a.Premium {
		return "", fmt.Errorf("ya tienes Premium activo; administra tu renovación en Cuenta")
	}
	if a.Admin {
		return "", fmt.Errorf("tu cuenta de administrador ya tiene todas las funciones sin pagar")
	}
	id := newID()
	tx, e := s.Store.Pool.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "INSERT INTO users(id) VALUES($1) ON CONFLICT DO NOTHING", user); e != nil {
		return "", e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO invoices(payload,user_id,stars) VALUES($1,$2,$3)", id, user, s.Config.PremiumStars); e != nil {
		return "", e
	}
	if e = tx.Commit(ctx); e != nil {
		return "", e
	}
	var link string
	e = s.Bot.Call(ctx, "createInvoiceLink", map[string]any{"title": "Webrom Premium · 30 días", "description": "🇪🇸 30 días: 100 YouTube + 300 redes/archivos, 4K, recortes y partes. Renovación automática; cancela en Cuenta. 🇬🇧 30 days: 100 YouTube + 300 social/files, 4K, trimming & splitting. Auto-renews; cancel in Account.", "payload": id, "currency": "XTR", "prices": []map[string]any{{"label": "Premium 30 días", "amount": s.Config.PremiumStars}}, "subscription_period": 2592000}, &link)
	return link, e
}
func (s *Store) ValidateCheckout(ctx context.Context, p telegram.PreCheckout) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", p.From.ID); err != nil {
		return err
	}
	var valid bool
	e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM invoices WHERE payload=$1 AND user_id=$2 AND stars=$3 AND created_at>now()-interval '1 hour')", p.Payload, p.From.ID, p.Amount).Scan(&valid)
	if e != nil {
		return e
	}
	if !valid || p.Currency != "XTR" {
		return fmt.Errorf("factura inválida o vencida; vuelve a abrir Premium")
	}
	a, e := s.account(ctx, tx, p.From.ID)
	if e != nil {
		return e
	}
	if a.Premium {
		return fmt.Errorf("ya tienes una suscripción activa")
	}
	var other bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM invoices WHERE user_id=$1 AND payload<>$2 AND approved_at>now()-interval '5 minutes')", p.From.ID, p.Payload).Scan(&other); e != nil {
		return e
	}
	if other {
		return fmt.Errorf("ya hay otro pago pendiente; espera antes de volver a contratar")
	}
	if _, e = tx.Exec(ctx, "UPDATE invoices SET approved_at=now() WHERE payload=$1", p.Payload); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) ApplyPayment(ctx context.Context, user int64, p telegram.Payment) error {
	if p.Currency != "XTR" || p.ChargeID == "" || p.Expires <= 0 || !p.Recurring {
		return fmt.Errorf("pago de suscripción inválido")
	}
	// UNIQUE(charge_id) makes retried updates harmless. Each renewal has its own charge.
	r, e := s.Pool.Exec(ctx, `INSERT INTO subscriptions(charge_id,user_id,payload,stars,expires_at)
 SELECT $1,$2,$3,$4,$5 FROM invoices WHERE payload=$3 AND user_id=$2 AND stars=$4
 ON CONFLICT(charge_id) DO NOTHING`, p.ChargeID, user, p.Payload, p.Amount, time.Unix(p.Expires, 0))
	if e != nil {
		return e
	}
	if r.RowsAffected() == 0 {
		var exists bool
		e = s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM subscriptions WHERE charge_id=$1 AND user_id=$2)", p.ChargeID, user).Scan(&exists)
		if e != nil {
			return e
		}
		if !exists {
			return fmt.Errorf("pago sin factura coincidente: revisar soporte")
		}
	}
	return nil
}
func (s *Server) cancelSubscription(ctx context.Context, user int64) error {
	var charge string
	e := s.Store.Pool.QueryRow(ctx, "SELECT charge_id FROM subscriptions WHERE user_id=$1 AND expires_at>now() AND NOT refunded ORDER BY expires_at DESC LIMIT 1", user).Scan(&charge)
	if e != nil {
		return fmt.Errorf("no tienes una suscripción activa")
	}
	if s.Bot == nil {
		return fmt.Errorf("Telegram no configurado")
	}
	if e = s.Bot.Call(ctx, "editUserStarSubscription", map[string]any{"user_id": user, "telegram_payment_charge_id": charge, "is_canceled": true}, nil); e != nil {
		return e
	}
	_, e = s.Store.Pool.Exec(ctx, "UPDATE subscriptions SET cancelled=true WHERE user_id=$1 AND NOT refunded", user)
	return e
}
