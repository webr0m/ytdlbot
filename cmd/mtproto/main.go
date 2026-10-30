// Optional owner-only MTProto downloader. Login is performed by the owner in a
// local terminal; codes and 2FA passwords are never exposed by the web service.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
	"golang.org/x/term"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"webrom/ytdlbot/internal/app"
)

type localAuth struct{}

func hidden(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, e := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return strings.TrimSpace(string(b)), e
}
func (localAuth) Phone(context.Context) (string, error) {
	return hidden("Teléfono con prefijo (solo local): ")
}
func (localAuth) Password(context.Context) (string, error) {
	return hidden("Contraseña 2FA (solo local): ")
}
func (localAuth) Code(context.Context, *tg.AuthSentCode) (string, error) {
	return hidden("Código Telegram (solo local): ")
}
func (localAuth) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, fmt.Errorf("solo cuentas existentes")
}
func (localAuth) AcceptTermsOfService(context.Context, tg.HelpTermsOfService) error {
	return fmt.Errorf("debes aceptar términos desde Telegram")
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "MTProto:", e)
		os.Exit(1)
	}
}
func run() error {
	login := flag.Bool("login", false, "Iniciar sesión interactivamente en terminal local")
	raw := flag.String("url", "", "Enlace https://t.me/canal/mensaje (solo canales accesibles)")
	out := flag.String("out", "", "Directorio de salida")
	upload := flag.String("upload", "", "Archivo para enviar a Mensajes guardados")
	manifest := flag.String("upload-manifest", "", "Manifiesto de partes para unir virtualmente y enviar")
	owner := flag.Int64("owner", 0, "ID del propietario destinatario; debe coincidir con la sesión")
	flag.Parse()
	app.LoadEnv(".env")
	id, e := strconv.Atoi(os.Getenv("TELEGRAM_API_ID"))
	if e != nil || id <= 0 || os.Getenv("TELEGRAM_API_HASH") == "" {
		return fmt.Errorf("configura TELEGRAM_API_ID y TELEGRAM_API_HASH en .env")
	}
	os.MkdirAll("secrets", 0700)
	client := telegram.NewClient(id, os.Getenv("TELEGRAM_API_HASH"), telegram.Options{Logger: diagnosticLogger(), SessionStorage: &session.FileStorage{Path: filepath.Join("secrets", "mtproto-session.json")}, DialTimeout: 15 * time.Second, Middlewares: []telegram.Middleware{telegram.MiddlewareFunc(func(next tg.Invoker) telegram.InvokeFunc {
		return func(ctx context.Context, in bin.Encoder, out bin.Decoder) error {
			rpcCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			return next.Invoke(rpcCtx, in, out)
		}
	})}})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()
	connectTimer := time.AfterFunc(60*time.Second, cancel)
	defer connectTimer.Stop()
	return client.Run(ctx, func(ctx context.Context) error {
		connectTimer.Stop()
		json.NewEncoder(os.Stderr).Encode(map[string]any{"phase": "connected"})
		if *login {
			if e := client.Auth().IfNecessary(ctx, auth.NewFlow(localAuth{}, auth.SendCodeOptions{})); e != nil {
				return e
			}
		}
		status, e := client.Auth().Status(ctx)
		if e != nil {
			return e
		}
		if !status.Authorized {
			return fmt.Errorf("sesión pendiente; ejecuta bin/mtproto.exe -login en una terminal local")
		}
		self, e := client.Self(ctx)
		if e != nil {
			return e
		}
		ownerAllowed := false
		for _, value := range strings.Split(os.Getenv("ADMIN_IDS"), ",") {
			owner, parseErr := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if parseErr == nil && owner == self.ID {
				ownerAllowed = true
			}
		}
		if !ownerAllowed {
			return fmt.Errorf("la sesion no pertenece a un administrador configurado")
		}
		if *upload != "" || *manifest != "" {
			json.NewEncoder(os.Stderr).Encode(map[string]any{"phase": "authenticated", "premium": self.Premium})
			if *raw != "" || (*upload != "" && *manifest != "") {
				return fmt.Errorf("selecciona una única fuente de carga")
			}
			return uploadSaved(ctx, client, self, *owner, *upload, *manifest)
		}
		if *raw == "" {
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"authorized": true, "premium": self.Premium})
		}
		if *out == "" {
			return fmt.Errorf("falta -out")
		}
		u, e := url.Parse(*raw)
		if e != nil || u.Scheme != "https" || u.Host != "t.me" {
			return fmt.Errorf("usa https://t.me/canal/mensaje")
		}
		p := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(p) != 2 || p[0] == "c" || p[0] == "s" {
			return fmt.Errorf("solo enlaces de canales con nombre público; no se unen canales automáticamente")
		}
		mid, e := strconv.Atoi(p[1])
		if e != nil || mid <= 0 {
			return fmt.Errorf("ID de mensaje inválido")
		}
		api := client.API()
		resolved, e := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: p[0]})
		if e != nil {
			return e
		}
		var channel *tg.Channel
		for _, ch := range resolved.Chats {
			if c, ok := ch.(*tg.Channel); ok {
				channel = c
				break
			}
		}
		if channel == nil {
			return fmt.Errorf("el enlace no es de un canal")
		}
		result, e := api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{Channel: &tg.InputChannel{ChannelID: channel.ID, AccessHash: channel.AccessHash}, ID: []tg.InputMessageClass{&tg.InputMessageID{ID: mid}}})
		if e != nil {
			return e
		}
		var messages []tg.MessageClass
		switch m := result.(type) {
		case *tg.MessagesChannelMessages:
			messages = m.Messages
		case *tg.MessagesMessages:
			messages = m.Messages
		case *tg.MessagesMessagesSlice:
			messages = m.Messages
		}
		for _, mc := range messages {
			m, ok := mc.(*tg.Message)
			if !ok {
				continue
			}
			if m.Noforwards {
				return fmt.Errorf("el canal restringe guardar este contenido")
			}
			media, ok := m.Media.(*tg.MessageMediaDocument)
			if !ok {
				continue
			}
			doc, ok := media.Document.(*tg.Document)
			if !ok {
				continue
			}
			name := "telegram_" + strconv.Itoa(mid) + ".bin"
			for _, attr := range doc.Attributes {
				if f, ok := attr.(*tg.DocumentAttributeFilename); ok {
					name = filepath.Base(strings.ReplaceAll(f.FileName, "\\", "/"))
				}
			}
			if name == "." || name == ".." || strings.ContainsAny(name, "/\\:") {
				return fmt.Errorf("nombre de archivo inválido")
			}
			os.MkdirAll(*out, 0700)
			path := filepath.Join(*out, name)
			if _, e = os.Stat(path); e == nil {
				return fmt.Errorf("archivo de salida ya existe")
			}
			threads := 2
			if v, parseErr := strconv.Atoi(os.Getenv("MTPROTO_DOWNLOAD_THREADS")); parseErr == nil && v >= 1 && v <= 8 {
				threads = v
			}
			_, e = downloader.NewDownloader().Download(api, &tg.InputDocumentFileLocation{ID: doc.ID, AccessHash: doc.AccessHash, FileReference: doc.FileReference}).WithThreads(threads).ToPath(ctx, path)
			return e
		}
		return fmt.Errorf("el mensaje no contiene un documento o vídeo descargable")
	})
}
