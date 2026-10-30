package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"

	"webrom/ytdlbot/internal/telegram"
)

const MaxFolders = 5

type folderChat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Username string `json:"username"`
}

func (s *Server) premiumAccess(ctx context.Context, user int64) (bool, error) {
	a, e := s.Store.Account(ctx, user)
	if e != nil {
		return false, e
	}
	return a.Admin || a.Premium, nil
}

func (s *Server) botUserID(ctx context.Context) (int64, error) {
	if v := s.botIDF.Load(); v > 0 {
		return int64(v), nil
	}
	var me struct {
		ID int64 `json:"id"`
	}
	if e := s.Bot.Call(ctx, "getMe", map[string]any{}, &me); e != nil {
		return 0, e
	}
	s.botIDF.Store(int64(me.ID))
	return me.ID, nil
}

func (s *Server) getFolderChat(ctx context.Context, chatID int64) (*folderChat, error) {
	var info struct {
		ID       int64  `json:"id"`
		Type     string `json:"type"`
		Title    string `json:"title"`
		Username string `json:"username"`
	}
	if e := s.Bot.Call(ctx, "getChat", map[string]any{"chat_id": chatID}, &info); e != nil {
		return nil, fmt.Errorf("no se pudo leer el destino; comprueba que el bot sea miembro del canal o grupo")
	}
	if info.Type != "channel" && info.Type != "group" && info.Type != "supergroup" {
		return nil, fmt.Errorf("el destino no es un canal ni un grupo")
	}
	botID, e := s.botUserID(ctx)
	if e != nil {
		return nil, e
	}
	switch info.Type {
	case "channel":
		var admins []struct {
			User struct {
				ID int64 `json:"id"`
			} `json:"user"`
			Status      string `json:"status"`
			Permissions *struct {
				CanPostMessages bool `json:"can_post_messages"`
			} `json:"permissions"`
		}
		if e = s.Bot.Call(ctx, "getChatAdministrators", map[string]any{"chat_id": chatID}, &admins); e != nil {
			return nil, fmt.Errorf("no se pudo consultar los administradores del canal")
		}
		found := false
		for _, a := range admins {
			if a.User.ID != botID {
				continue
			}
			found = true
			if a.Status != "administrator" && a.Status != "creator" {
				return nil, fmt.Errorf("el bot no tiene permisos para publicar en este canal")
			}
			if !a.Permissions.CanPostMessages {
				return nil, fmt.Errorf("el bot no tiene permisos para publicar en este canal")
			}
		}
		if !found {
			return nil, fmt.Errorf("el bot no es administrador del canal; pídele a tu usuario que añada @mutidlprobot")
		}
	case "group", "supergroup":
		var member struct {
			Status      string `json:"status"`
			Permissions *struct {
				CanSendMessages bool `json:"can_send_messages"`
			} `json:"permissions"`
		}
		if e = s.Bot.Call(ctx, "getChatMember", map[string]any{"chat_id": chatID, "user_id": botID}, &member); e != nil {
			return nil, fmt.Errorf("no se pudo consultar los permisos del bot en el grupo")
		}
		switch member.Status {
		case "member", "administrator", "creator":
		case "restricted":
			if member.Permissions == nil || !member.Permissions.CanSendMessages {
				return nil, fmt.Errorf("el bot no tiene permisos para enviar en este grupo")
			}
		default:
			return nil, fmt.Errorf("el bot no es miembro del grupo")
		}
	}
	return &folderChat{ID: info.ID, Type: info.Type, Title: info.Title, Username: info.Username}, nil
}

func (s *Server) CreateFolderManual(ctx context.Context, user int64, name, chatRef string) (*Folder, error) {
	ref := strings.TrimSpace(chatRef)
	if ref == "" {
		return nil, fmt.Errorf("indica el canal o grupo")
	}
	if u, e := url.Parse("https://" + ref); e == nil && u.Host == "t.me" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) > 0 && parts[0] != "" {
			ref = parts[0]
		}
	}
	var info struct {
		ID       int64  `json:"id"`
		Type     string `json:"type"`
		Title    string `json:"title"`
		Username string `json:"username"`
	}
	if err := s.Bot.Call(ctx, "getChat", map[string]any{"chat_id": ref}, &info); err != nil {
		return nil, fmt.Errorf("no se pudo resolver el canal o grupo; usa el selector nativo o un @usuario")
	}
	if info.Type != "channel" && info.Type != "group" && info.Type != "supergroup" {
		return nil, fmt.Errorf("el destino no es un canal ni un grupo")
	}
	resolved, err := s.getFolderChat(ctx, info.ID)
	if err != nil {
		return nil, err
	}
	label := strings.TrimSpace(name)
	if label == "" {
		label = resolved.Title
	}
	if label == "" {
		label = fmt.Sprintf("Carpeta %d", info.ID)
	}
	label = strings.TrimSpace(label)
	if len(label) > 64 {
		label = label[:64]
	}
	return s.Store.CreateFolder(ctx, user, label, resolved.Type, resolved.ID, resolved.Title, resolved.Username)
}

func (s *Server) CompleteFolderFromChatShared(ctx context.Context, user int64, requestID int64, chatID int64) (*Folder, error) {
	premium, e := s.premiumAccess(ctx, user)
	if e != nil {
		return nil, e
	}
	if !premium {
		return nil, fmt.Errorf("las carpetas son una función Premium")
	}
	_, e = s.Store.FolderRequest(ctx, requestID, user)
	if e != nil {
		return nil, fmt.Errorf("la selección de carpeta expiró; abre el selector de nuevo")
	}
	if err := s.Store.ConsumeFolderRequest(ctx, requestID, user); err != nil {
		return nil, err
	}
	chat, err := s.getFolderChat(ctx, chatID)
	if err != nil {
		return nil, err
	}
	label := chat.Title
	if label == "" {
		label = fmt.Sprintf("Carpeta %d", chat.ID)
	}
	return s.Store.CreateFolder(ctx, user, label, chat.Type, chat.ID, chat.Title, chat.Username)
}

func (s *Server) DeliverJobToFolder(ctx context.Context, user telegram.User, j Job, folder *Folder, out Output) error {
	if folder.UserID != user.ID {
		return fmt.Errorf("la carpeta no pertenece a la cuenta")
	}
	premium, e := s.premiumAccess(ctx, user.ID)
	if e != nil {
		return e
	}
	if !premium {
		return fmt.Errorf("las carpetas son una función Premium")
	}
	if out.FileID != "" {
		kind := out.Kind
		if kind != "video" && kind != "audio" {
			kind = "document"
		}
		payload := map[string]any{"chat_id": folder.ChatID, kind: out.FileID}
		if kind == "video" {
			payload["supports_streaming"] = true
		}
		var sent struct {
			MessageID int64 `json:"message_id"`
		}
		if e = s.Bot.Call(ctx, "send"+strings.ToUpper(kind[:1])+kind[1:], payload, &sent); e != nil {
			return fmt.Errorf("no se pudo enviar el archivo a la carpeta; revisa los permisos del bot")
		}
		return nil
	}
	if out.SavedMessageID <= 0 {
		return fmt.Errorf("el archivo no tiene una referencia para moverse a una carpeta")
	}
	args := []string{
		"-forward",
		"-owner", strconv.FormatInt(user.ID, 10),
		"-saved-message", strconv.Itoa(int(out.SavedMessageID)),
		"-chat", strconv.FormatInt(folder.ChatID, 10),
	}
	if folder.ChatUsername != "" {
		args = append(args, "-chat-username", strings.TrimPrefix(folder.ChatUsername, "@"))
	}
	cmd := exec.CommandContext(ctx, s.Config.MTProto, args...)
	prepareProcess(cmd)
	var result bytes.Buffer
	var diagnostic tail
	cmd.Stdout = &result
	cmd.Stderr = &diagnostic
	if e = s.uploadSlotErr(ctx, func() error { return cmd.Run() }); e != nil {
		return fmt.Errorf("no se pudo mover el archivo con la sesión Premium del propietario")
	}
	var forwarded struct {
		Forwarded bool  `json:"forwarded"`
		MessageID int64 `json:"messageId"`
	}
	if e = json.Unmarshal(result.Bytes(), &forwarded); e != nil || !forwarded.Forwarded || forwarded.MessageID <= 0 {
		return fmt.Errorf("la sesión Premium no confirmó la transferencia al canal")
	}
	return nil
}

func (s *Server) cleanupFolderRequests(ctx context.Context) error {
	_, e := s.Store.Pool.Exec(ctx, "DELETE FROM folder_requests WHERE created_at < now() - interval '10 minutes'")
	return e
}
