package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type Options struct {
	Mode         string `json:"mode"`
	Quality      int    `json:"quality"`
	AudioFormat  string `json:"audioFormat"`
	Engine       string `json:"engine"`
	Playlist     bool   `json:"playlist"`
	Subtitles    bool   `json:"subtitles"`
	Start        int    `json:"start"`
	End          int    `json:"end"`
	UseCookies   bool   `json:"useCookies"`
	AsDocument   bool   `json:"asDocument"`
	Split        bool   `json:"split"`
	TranscodeRAM bool   `json:"transcodeRam"`
}

func (o *Options) Validate() error {
	if o.Mode == "" {
		o.Mode = "video"
	}
	if o.Engine == "" {
		o.Engine = "auto"
	}
	// Quality 0 means "original/best" for video mode.
	if o.AudioFormat == "" {
		o.AudioFormat = "mp3"
	}
	if o.Mode != "video" && o.Mode != "audio" && o.Mode != "file" && o.Mode != "gallery" {
		return fmt.Errorf("modo inválido")
	}
	if o.Engine != "auto" && o.Engine != "ytdlp" && o.Engine != "direct" && o.Engine != "gallery" {
		return fmt.Errorf("motor inválido")
	}
	if o.Quality != 0 && o.Quality != 360 && o.Quality != 480 && o.Quality != 720 && o.Quality != 1080 && o.Quality != 1440 && o.Quality != 2160 {
		return fmt.Errorf("calidad inválida")
	}
	if o.AudioFormat != "mp3" && o.AudioFormat != "m4a" && o.AudioFormat != "flac" && o.AudioFormat != "opus" {
		return fmt.Errorf("formato de audio inválido")
	}
	if o.Start < 0 || o.End < 0 || o.Start > 86400 || o.End > 86400 || (o.End != 0 && o.End <= o.Start) {
		return fmt.Errorf("recorte inválido: fin debe ser mayor al inicio (segundos)")
	}
	if o.Mode == "gallery" {
		o.Engine = "gallery"
	}
	if o.Mode == "file" {
		o.Engine = "direct"
	}
	if o.TranscodeRAM && o.Mode != "video" {
		return fmt.Errorf("transcodificación RAM disponible para vídeo")
	}
	if o.TranscodeRAM {
		o.Split = true
	}
	return nil
}

type Job struct {
	Stage     string    `json:"stage"`
	Detail    string    `json:"detail"`
	ID        string    `json:"id"`
	UserID    int64     `json:"userId"`
	ChatID    int64     `json:"-"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	Progress  float64   `json:"progress"`
	Options   Options   `json:"options"`
	Error     string    `json:"error,omitempty"`
	Files     []Output  `json:"files"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Worker    string    `json:"-"`
}
type Output struct {
	Name           string `json:"name"`
	Size           int64  `json:"size"`
	FileID         string `json:"fileId,omitempty"`
	Kind           string `json:"kind,omitempty"`
	SavedMessageID int    `json:"savedMessageId,omitempty"`
}
type MediaInfo struct {
	Title     string   `json:"title"`
	Extractor string   `json:"extractor"`
	Duration  float64  `json:"duration"`
	Formats   []Format `json:"formats"`
}
type Format struct {
	ID     string  `json:"format_id"`
	Ext    string  `json:"ext"`
	Height int     `json:"height"`
	Size   float64 `json:"filesize"`
	VCodec string  `json:"vcodec"`
	ACodec string  `json:"acodec"`
}

func newID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
