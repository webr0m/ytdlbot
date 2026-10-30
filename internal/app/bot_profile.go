package app

import (
	"context"
	"net/url"
	"strings"
	"webrom/ytdlbot/internal/telegram"
)

var botCommands = [][3]string{
	{"start", "🚀 Abrir Mini App", "🚀 Open Mini App"}, {"download", "⬇️ Nueva descarga", "⬇️ New download"}, {"queue", "📚 Cola y progreso", "📚 Queue and progress"}, {"history", "🕘 Historial y archivos", "🕘 History and files"}, {"premium", "⭐ Planes y suscripción", "⭐ Plans and subscription"}, {"account", "👤 Mi cuenta y límites", "👤 Account and limits"}, {"settings", "⚙️ Ajustes de la app", "⚙️ App settings"}, {"folders", "📁 Mis carpetas", "📁 My folders"}, {"help", "❓ Ayuda y soporte", "❓ Help and support"}, {"terms", "📃 Condiciones de uso", "📃 Terms of use"}, {"privacy", "🔒 Privacidad", "🔒 Privacy policy"}, {"paysupport", "💬 Pagos y reembolsos", "💬 Payments and refunds"},
}

func (s *Server) configureCommands(ctx context.Context) {
	for _, lang := range []string{"", "es", "en"} {
		commands := []map[string]string{}
		for _, c := range botCommands {
			description := c[1] + " / " + c[2]
			if lang == "es" {
				description = "🇪🇸 " + c[1]
			}
			if lang == "en" {
				description = "🇬🇧 " + c[2]
			}
			commands = append(commands, map[string]string{"command": c[0], "description": description})
		}
		s.Bot.Call(ctx, "setMyCommands", map[string]any{"language_code": lang, "commands": commands}, nil)
	}
}
func (s *Server) menuFor(user telegram.User, view string) any {
	if s.Config.PublicURL == "" {
		return nil
	}
	lang := "en"
	label := "🇬🇧 Open app"
	if strings.HasPrefix(user.LanguageCode, "es") {
		lang = "es"
		label = "🇪🇸 Abrir app"
	}
	u, _ := url.Parse(s.Config.PublicURL)
	q := u.Query()
	q.Set("lang", lang)
	q.Set("view", view)
	u.RawQuery = q.Encode()
	return map[string]any{"inline_keyboard": [][]map[string]any{{{"text": label, "web_app": map[string]string{"url": u.String()}}}}}
}
