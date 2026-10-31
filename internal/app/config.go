package app

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr, DatabaseURL, Token, BotUsername, PublicURL, TelegramURL, DataDir string
	YTDLP, GalleryDL, FFmpeg, Cookies, Proxy, LocalKey, SupportURL         string
	Aria2, Streamlink, NM3U8DL                                            string
	Payments                                                               bool
	PremiumStars                                                           int
	RAMPartMB, FFmpegThreads                                               int
	MinFreeBytes                                                           uint64
	MTProto                                                                string
	SavedUploads                                                           bool
	Admins, Allowed                                                        map[int64]bool
	Public, Gallery                                                        bool
	Workers, MaxPending, DailyLimit, PlaylistLimit                         int
	MaxActivePerUser, QueueLimit, DBMaxConns, UploadConcurrency            int
	MaxBytes                                                               int64
	Timeout, Retention                                                     time.Duration
}

func LoadEnv(path string) error {
	f, e := os.Open(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), "\"'")
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
	return s.Err()
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func integer(k string, d int) int {
	v, e := strconv.Atoi(env(k, strconv.Itoa(d)))
	if e != nil {
		return d
	}
	return v
}
func ids(k string) map[int64]bool {
	m := map[int64]bool{}
	for _, s := range strings.Split(os.Getenv(k), ",") {
		v, e := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if e == nil && v > 0 {
			m[v] = true
		}
	}
	return m
}
func LoadConfig() (Config, error) {
	c := Config{Addr: env("APP_ADDR", "127.0.0.1:8095"), DatabaseURL: os.Getenv("DATABASE_URL"), Token: os.Getenv("BOT_TOKEN"), BotUsername: os.Getenv("BOT_USERNAME"), PublicURL: strings.TrimRight(os.Getenv("PUBLIC_URL"), "/"), TelegramURL: strings.TrimRight(env("TELEGRAM_API_URL", "http://127.0.0.1:8081"), "/"), DataDir: env("DATA_DIR", "./data"), YTDLP: env("YTDLP_PATH", "yt-dlp"), GalleryDL: env("GALLERYDL_PATH", "gallery-dl"), FFmpeg: env("FFMPEG_PATH", "ffmpeg"), Aria2: env("ARIA2_PATH", ""), Streamlink: env("STREAMLINK_PATH", ""), NM3U8DL: env("NM3U8DL_PATH", ""), Cookies: os.Getenv("COOKIES_FILE"), Proxy: os.Getenv("DOWNLOAD_PROXY"), LocalKey: os.Getenv("LOCAL_ADMIN_KEY"), Admins: ids("ADMIN_IDS"), Allowed: ids("ALLOWED_USER_IDS"), Public: env("PUBLIC_ACCESS", "false") == "true", Gallery: env("ENABLE_GALLERY", "true") == "true", Workers: integer("WORKERS", 2), MaxPending: integer("MAX_PENDING_PER_USER", 5), MaxActivePerUser: integer("MAX_ACTIVE_PER_USER", 2), QueueLimit: integer("MAX_QUEUE", 1000), DBMaxConns: integer("DB_MAX_CONNS", 16), UploadConcurrency: integer("MAX_CONCURRENT_UPLOADS", 2), DailyLimit: integer("DAILY_LIMIT", 50), PlaylistLimit: integer("MAX_PLAYLIST_ITEMS", 20), MaxBytes: int64(integer("MAX_FILE_MB", 1900)) * 1000 * 1000, Timeout: time.Duration(integer("JOB_TIMEOUT_MINUTES", 90)) * time.Minute, Retention: time.Duration(integer("RETENTION_HOURS", 24)) * time.Hour}
	if c.Token != "" && c.Admins[1] {
		return c, fmt.Errorf("reemplaza ADMIN_IDS=1 por tu ID real antes de activar Telegram")
	}
	c.Payments = env("ENABLE_PAYMENTS", "false") == "true"
	c.PremiumStars = integer("PREMIUM_STARS", 50)
	c.SupportURL = os.Getenv("SUPPORT_URL")
	c.RAMPartMB = integer("RAM_PART_MB", 512)
	c.FFmpegThreads = integer("FFMPEG_THREADS", 2)
	c.MinFreeBytes = uint64(integer("MIN_FREE_DISK_MB", 4096)) * 1000 * 1000
	c.MTProto = os.Getenv("MTPROTO_PATH")
	c.SavedUploads = env("MTPROTO_SAVED_UPLOADS", "false") == "true"
	if c.RAMPartMB < 16 || c.RAMPartMB > 1900 || int64(c.RAMPartMB)*1000*1000 > c.MaxBytes || c.FFmpegThreads < 1 || c.FFmpegThreads > 8 {
		return c, fmt.Errorf("RAM_PART_MB debe ser 16..1900 y <=MAX_FILE_MB; FFMPEG_THREADS debe ser 1..8")
	}
	if c.PremiumStars < 1 || c.PremiumStars > 10000 {
		return c, fmt.Errorf("PREMIUM_STARS inválido")
	}
	validSupport := strings.HasPrefix(c.SupportURL, "https://t.me/")
	if support, err := url.Parse(c.SupportURL); err == nil && support.Scheme == "tg" && support.Host == "user" {
		owner, _ := strconv.ParseInt(support.Query().Get("id"), 10, 64)
		validSupport = c.Admins[owner]
	}
	if c.Payments && (c.Token == "" || c.PublicURL == "" || !validSupport) {
		return c, fmt.Errorf("pagos requieren BOT_TOKEN, PUBLIC_URL y SUPPORT_URL de Telegram")
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("falta DATABASE_URL; ejecuta scripts/setup.ps1")
	}
	if c.Workers < 1 || c.Workers > maxConcurrentJobs || c.MaxPending < 1 || c.DailyLimit < 1 || c.PlaylistLimit < 1 || c.PlaylistLimit > 100 || c.Timeout < time.Minute || c.Retention < time.Hour || c.MaxBytes < 1000000 || c.MaxBytes > 2000*1000*1000 {
		return c, fmt.Errorf("límites de configuración inválidos")
	}
	if c.MaxActivePerUser < 1 || c.MaxActivePerUser > maxConcurrentJobs {
		return c, fmt.Errorf("MAX_ACTIVE_PER_USER debe estar entre 1 y %d", maxConcurrentJobs)
	}
	if c.UploadConcurrency < 1 || c.UploadConcurrency > 8 {
		return c, fmt.Errorf("MAX_CONCURRENT_UPLOADS debe estar entre 1 y 8")
	}
	if c.DBMaxConns < 4 || c.DBMaxConns > 64 {
		return c, fmt.Errorf("DB_MAX_CONNS debe estar entre 4 y 64")
	}
	if c.QueueLimit < 10 || c.QueueLimit > 10000 {
		return c, fmt.Errorf("MAX_QUEUE debe estar entre 10 y 10000")
	}
	if c.LocalKey != "" && len(c.LocalKey) < 32 {
		return c, fmt.Errorf("LOCAL_ADMIN_KEY debe tener al menos 32 caracteres")
	}
	if c.PublicURL != "" {
		u, e := url.Parse(c.PublicURL)
		if e != nil || u.Scheme != "https" || u.Host == "" {
			return c, fmt.Errorf("PUBLIC_URL debe ser HTTPS")
		}
	}
	u, e := url.Parse(c.TelegramURL)
	if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return c, fmt.Errorf("TELEGRAM_API_URL inválida")
	}
	if u.Hostname() == "api.telegram.org" && c.MaxBytes > 50*1000*1000 {
		return c, fmt.Errorf("API cloud: configura MAX_FILE_MB<=50 o usa la API local")
	}
	return c, nil
}
func (c Config) CanUse(id int64) bool { return c.Public || c.Admins[id] || c.Allowed[id] }
