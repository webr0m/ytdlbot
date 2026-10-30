package telegram

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type User struct {
	ID           int64  `json:"id"`
	FirstName    string `json:"first_name"`
	Username     string `json:"username"`
	LanguageCode string `json:"language_code,omitempty"`
}

func ValidateInitData(raw, token string, now time.Time) (User, error) {
	var user User
	if token == "" || len(raw) > 16384 {
		return user, fmt.Errorf("sesión inválida")
	}
	v, e := url.ParseQuery(raw)
	if e != nil {
		return user, fmt.Errorf("sesión inválida")
	}
	for _, vs := range v {
		if len(vs) != 1 {
			return user, fmt.Errorf("campo duplicado")
		}
	}
	hash, e := hex.DecodeString(v.Get("hash"))
	if e != nil || len(hash) != 32 {
		return user, fmt.Errorf("firma inválida")
	}
	v.Del("hash")
	lines := make([]string, 0, len(v))
	for k := range v {
		lines = append(lines, k+"="+v.Get(k))
	}
	sort.Strings(lines)
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	if !hmac.Equal(hash, mac.Sum(nil)) {
		return user, fmt.Errorf("firma inválida")
	}
	timestamp, e := strconv.ParseInt(v.Get("auth_date"), 10, 64)
	age := now.Sub(time.Unix(timestamp, 0))
	if e != nil || age < -30*time.Second || age > 24*time.Hour {
		return user, fmt.Errorf("sesión vencida: vuelve a abrir la Mini App")
	}
	if json.Unmarshal([]byte(v.Get("user")), &user) != nil || user.ID <= 0 {
		return user, fmt.Errorf("usuario inválido")
	}
	return user, nil
}
