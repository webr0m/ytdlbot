package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"webrom/ytdlbot/internal/virtualfile"
)

type uploadProgress struct {
	mu   sync.Mutex
	last time.Time
}

func (p *uploadProgress) Chunk(ctx context.Context, s uploader.ProgressState) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Since(p.last) > time.Second || s.Uploaded == s.Total {
		json.NewEncoder(os.Stderr).Encode(map[string]any{"uploaded": s.Uploaded, "total": s.Total})
		p.last = time.Now()
	}
	return ctx.Err()
}

// The authenticated owner can upload only to their own Saved Messages.
func uploadSaved(ctx context.Context, client *telegram.Client, self *tg.User, owner int64, path, manifest string) error {
	if owner != self.ID {
		return fmt.Errorf("Mensajes guardados solo está disponible para el propietario de la sesión")
	}
	var r io.ReadCloser
	var name string
	var size int64
	if manifest != "" {
		json.NewEncoder(os.Stderr).Encode(map[string]any{"phase": "verifying"})
		v, e := virtualfile.Open(ctx, manifest)
		if e != nil {
			return e
		}
		r = v.Reader(ctx)
		name = v.Manifest.Original
		size = v.Manifest.Size
	} else {
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		info, e := f.Stat()
		if e != nil {
			f.Close()
			return e
		}
		if !info.Mode().IsRegular() {
			f.Close()
			return fmt.Errorf("archivo inválido")
		}
		r = f
		name = filepath.Base(path)
		size = info.Size()
	}
	defer r.Close()
	max := int64(2 * 1024 * 1024 * 1024)
	if self.Premium {
		max = 4 * 1024 * 1024 * 1024
	}
	if size <= 0 || size > max {
		return fmt.Errorf("el archivo unido excede el límite de la cuenta Telegram")
	}
	api := client.API()
	json.NewEncoder(os.Stderr).Encode(map[string]any{"phase": "uploading", "total": size})
	threads, _ := strconv.Atoi(os.Getenv("MTPROTO_UPLOAD_THREADS"))
	if threads < 1 || threads > 8 {
		threads = 4
	}
	pool, e := client.Pool(4)
	if e != nil {
		return e
	}
	defer pool.Close()
	uploadAPI := tg.NewClient(telegram.InvokeFunc(func(ctx context.Context, in bin.Encoder, out bin.Decoder) error {
		rpcCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		return pool.Invoke(rpcCtx, in, out)
	}))
	file, e := uploader.NewUploader(uploadAPI).WithThreads(threads).WithPartSize(512*1024).WithProgress(&uploadProgress{}).Upload(ctx, uploader.NewUpload(name, r, size))
	if e != nil {
		return e
	}
	var b [8]byte
	if _, e = rand.Read(b[:]); e != nil {
		return e
	}
	randomID := int64(binary.LittleEndian.Uint64(b[:]))
	updates, e := api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{Peer: &tg.InputPeerSelf{}, Media: &tg.InputMediaUploadedDocument{File: file, MimeType: "application/octet-stream", Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeFilename{FileName: name}}}, Message: "Multi Downloader Pro · archivo completo", RandomID: randomID})
	if e != nil {
		return e
	}
	mid := 0
	switch u := updates.(type) {
	case *tg.UpdateShortSentMessage:
		mid = u.ID
	case *tg.Updates:
		mid = sentMessageID(u.Updates, randomID)
	case *tg.UpdatesCombined:
		mid = sentMessageID(u.Updates, randomID)
	}
	if mid == 0 {
		return fmt.Errorf("Telegram no confirmó el ID del mensaje; revisa Mensajes guardados antes de reintentar")
	}
	// Read the acknowledged message back: a saved upload is successful only when
	// Telegram reports the expected name and complete document size.
	result, e := api.MessagesGetMessages(ctx, []tg.InputMessageClass{&tg.InputMessageID{ID: mid}})
	if e != nil {
		return e
	}
	var messages []tg.MessageClass
	switch m := result.(type) {
	case *tg.MessagesMessages:
		messages = m.Messages
	case *tg.MessagesMessagesSlice:
		messages = m.Messages
	}
	for _, mc := range messages {
		if m, ok := mc.(*tg.Message); ok && m.ID == mid {
			if media, ok := m.Media.(*tg.MessageMediaDocument); ok {
				if d, ok := media.Document.(*tg.Document); ok && d.Size == size {
					for _, a := range d.Attributes {
						if n, ok := a.(*tg.DocumentAttributeFilename); ok && n.FileName == name {
							return json.NewEncoder(os.Stdout).Encode(map[string]any{"savedMessageId": mid, "name": name, "size": size, "premium": self.Premium, "verified": true})
						}
					}
				}
			}
		}
	}
	return fmt.Errorf("Telegram no confirmó el tamaño completo del archivo")
}
func sentMessageID(updates []tg.UpdateClass, randomID int64) int {
	for _, u := range updates {
		if id, ok := u.(*tg.UpdateMessageID); ok && id.RandomID == randomID {
			return id.ID
		}
	}
	for _, u := range updates {
		if m, ok := u.(*tg.UpdateNewMessage); ok {
			if msg, ok := m.Message.(*tg.Message); ok {
				return msg.ID
			}
		}
	}
	return 0
}

// forwardSaved moves a document delivered to the owner's Saved Messages into a
// destination channel or group using Telegram's file references, without another
// download or upload.
func forwardSaved(ctx context.Context, client *telegram.Client, self *tg.User, owner int64, savedMessageID int64, chatID int64, username string) error {
	if owner != self.ID {
		return fmt.Errorf("Mensajes guardados solo está disponible para el propietario de la sesión")
	}
	api := client.API()
	peer, err := resolveForwardPeer(ctx, api, chatID, username)
	if err != nil {
		return err
	}
	var b [8]byte
	if _, e := rand.Read(b[:]); e != nil {
		return e
	}
	randomID := int64(binary.LittleEndian.Uint64(b[:]))
	updates, e := api.MessagesForwardMessages(ctx, &tg.MessagesForwardMessagesRequest{
		FromPeer: &tg.InputPeerSelf{},
		ID:       []int{int(savedMessageID)},
		RandomID: []int64{randomID},
		ToPeer:   peer,
	})
	if e != nil {
		return e
	}
	mid := 0
	switch u := updates.(type) {
	case *tg.Updates:
		mid = sentMessageID(u.Updates, randomID)
	case *tg.UpdatesCombined:
		mid = sentMessageID(u.Updates, randomID)
	}
	if mid == 0 {
		return fmt.Errorf("Telegram no confirmó el reenvío al canal")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"forwarded": true, "messageId": mid, "chatId": chatID})
}

func resolveForwardPeer(ctx context.Context, api *tg.Client, chatID int64, username string) (tg.InputPeerClass, error) {
	if username != "" {
		resolved, e := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: username})
		if e != nil {
			return nil, e
		}
		var channel *tg.Channel
		for _, c := range resolved.Chats {
			if ch, ok := c.(*tg.Channel); ok {
				channel = ch
				break
			}
		}
		if channel != nil && !channel.Left {
			return &tg.InputPeerChannel{ChannelID: channel.ID, AccessHash: channel.AccessHash}, nil
		}
	}
	res, e := api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{OffsetPeer: &tg.InputPeerEmpty{}, Limit: 100, OffsetDate: 0, OffsetID: 0})
	if e != nil {
		return nil, e
	}
	var chats []tg.ChatClass
	switch d := res.(type) {
	case *tg.MessagesDialogs:
		chats = d.GetChats()
	case *tg.MessagesDialogsSlice:
		chats = d.GetChats()
	default:
		return nil, fmt.Errorf("la sesión Premium no devolvió la lista de chats")
	}
	for _, c := range chats {
		switch cc := c.(type) {
		case *tg.Channel:
			if cc.Left {
				continue
			}
			if cc.Broadcast {
				return &tg.InputPeerChannel{ChannelID: cc.ID, AccessHash: cc.AccessHash}, nil
			}
			return &tg.InputPeerChat{ChatID: cc.ID}, nil
		case *tg.Chat:
			if chatID < 0 {
				continue
			}
			return &tg.InputPeerChat{ChatID: cc.ID}, nil
		}
	}
	return nil, fmt.Errorf("no se encontró el canal o grupo destino en la sesión Premium")
}
