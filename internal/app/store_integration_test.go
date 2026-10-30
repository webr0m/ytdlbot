package app

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"webrom/ytdlbot/internal/telegram"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	LoadEnv(filepath.Join("..", "..", ".env"))
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("PostgreSQL local no configurado")
	}
	ctx := context.Background()
	p, e := pgxpool.New(ctx, raw)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	name := "test_" + newID()
	if _, e = p.Exec(ctx, "CREATE SCHEMA "+name); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Exec(ctx, "DROP SCHEMA "+name+" CASCADE") })
	u, _ := url.Parse(raw)
	q := u.Query()
	q.Set("search_path", name)
	u.RawQuery = q.Encode()
	c := Config{DatabaseURL: u.String(), MaxPending: 3, DailyLimit: 50, MaxActivePerUser: 2, QueueLimit: 1000, Admins: map[int64]bool{999: true}}
	s, e := OpenStore(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Pool.Close)
	return s
}
func TestPremiumJobsAreClaimedBeforeFreeOnes(t *testing.T) {
	s := testStore(t)
	s.Config.MaxActivePerUser = 1
	ctx := context.Background()
	free, e := s.Create(ctx, 201, 201, "https://example.com/free", Options{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, "INSERT INTO users(id) VALUES($1) ON CONFLICT DO NOTHING", 202); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, "INSERT INTO subscriptions(charge_id,user_id,payload,stars,expires_at) VALUES('premium-test',202,'test',50,now()+interval '30 days')"); e != nil {
		t.Fatal(e)
	}
	premium, e := s.Create(ctx, 202, 202, "https://example.com/premium", Options{Quality: 2160})
	if e != nil {
		t.Fatal(e)
	}
	first, e := s.Claim(ctx, "worker1")
	if e != nil {
		t.Fatal(e)
	}
	if first.ID != premium.ID {
		t.Fatal("una tarea gratuita adelantó a la Premium")
	}
	second, e := s.Claim(ctx, "worker2")
	if e != nil {
		t.Fatal(e)
	}
	if second.ID != free.ID {
		t.Fatal("no se repartió el siguiente espacio entre usuarios distintos")
	}
}

func TestAdminJobsAreClaimedBeforeFreeOnes(t *testing.T) {
	s := testStore(t)
	s.Config.MaxActivePerUser = 1
	ctx := context.Background()
	free, e := s.Create(ctx, 401, 401, "https://example.com/free", Options{})
	if e != nil {
		t.Fatal(e)
	}
	admin, e := s.Create(ctx, 999, 999, "https://example.com/admin", Options{})
	if e != nil {
		t.Fatal(e)
	}
	first, e := s.Claim(ctx, "worker1")
	if e != nil {
		t.Fatal(e)
	}
	if first.ID != admin.ID {
		t.Fatal("una tarea gratuita adelantó a la del administrador")
	}
	second, e := s.Claim(ctx, "worker2")
	if e != nil {
		t.Fatal(e)
	}
	if second.ID != free.ID {
		t.Fatal("la tarea gratuita no se ejecutó después de la del administrador")
	}
}

func TestClaimSpreadsUsersBeforeASecondJob(t *testing.T) {
	s := testStore(t)
	s.Config.MaxActivePerUser = 1
	ctx := context.Background()
	firstA, e := s.Create(ctx, 301, 301, "https://example.com/a1", Options{})
	if e != nil {
		t.Fatal(e)
	}
	secondA, e := s.Create(ctx, 301, 301, "https://example.com/a2", Options{})
	if e != nil {
		t.Fatal(e)
	}
	onlyB, e := s.Create(ctx, 302, 302, "https://example.com/b1", Options{})
	if e != nil {
		t.Fatal(e)
	}
	first, e := s.Claim(ctx, "worker1")
	if e != nil {
		t.Fatal(e)
	}
	if first.UserID != 301 {
		t.Fatal("el primer trabajo no siguió el orden de llegada")
	}
	second, e := s.Claim(ctx, "worker2")
	if e != nil {
		t.Fatal(e)
	}
	if second.ID != onlyB.ID {
		t.Fatal("un usuario acaparó dos espacios mientras otro esperaba")
	}
	if _, e = s.Pool.Exec(ctx, "UPDATE jobs SET state='done' WHERE id=$1", first.ID); e != nil {
		t.Fatal(e)
	}
	third, e := s.Claim(ctx, "worker3")
	if e != nil {
		t.Fatal(e)
	}
	expected := secondA.ID
	if first.ID == secondA.ID {
		expected = firstA.ID
	}
	if third.ID != expected {
		t.Fatal("el siguiente trabajo del mismo usuario no continuó tras liberar su espacio")
	}
}

func TestQueueQuotasAndPaymentIdempotency(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Create(ctx, 123, 123, "https://youtube.com/watch?v=test", Options{})
			if e == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 3 {
		t.Fatalf("límite concurrente: %d", success)
	}
	first, e := s.Claim(ctx, "worker1")
	if e != nil {
		t.Fatal(e)
	}
	second, e := s.Claim(ctx, "worker2")
	if e != nil || first.ID == second.ID {
		t.Fatal("claim duplicado", e)
	}
	if e = s.Cancel(ctx, first.ID, 456); e == nil {
		t.Fatal("cancelación ajena permitida")
	}
	if e = s.Cancel(ctx, first.ID, 123); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Create(ctx, 456, 456, "https://example.com/file", Options{Split: true}); e == nil {
		t.Fatal("split gratis permitido")
	}
	s.Pool.Exec(ctx, "INSERT INTO users(id) VALUES(456)")
	s.Pool.Exec(ctx, "INSERT INTO invoices(payload,user_id,stars) VALUES('invoice',456,50)")
	p := telegram.Payment{Currency: "XTR", Amount: 50, Payload: "invoice", ChargeID: "charge", Expires: time.Now().Add(30 * 24 * time.Hour).Unix(), Recurring: true}
	if e = s.ApplyPayment(ctx, 456, p); e != nil {
		t.Fatal(e)
	}
	if e = s.ApplyPayment(ctx, 456, p); e != nil {
		t.Fatal(e)
	}
	var count int
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM subscriptions WHERE user_id=456").Scan(&count)
	if count != 1 {
		t.Fatal("pago duplicado")
	}
	if _, e = s.Create(ctx, 456, 456, "https://example.com/file", Options{Split: true}); e != nil {
		t.Fatal(e)
	}
	p.Amount = 1
	p.ChargeID = "fake"
	if e = s.ApplyPayment(ctx, 456, p); e == nil {
		t.Fatal("importe fraudulento aceptado")
	}
}
