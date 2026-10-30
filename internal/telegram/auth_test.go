package telegram

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

func signed(now time.Time) string {
	v := url.Values{"auth_date": {fmt.Sprint(now.Unix())}, "user": {`{"id":123,"first_name":"Test"}`}, "signature": {"telegram-signature"}}
	lines := []string{}
	for k := range v {
		lines = append(lines, k+"="+v.Get(k))
	}
	sort.Strings(lines)
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte("token"))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	v.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return v.Encode()
}
func TestInitDataVerification(t *testing.T) {
	now := time.Now()
	valid := signed(now)
	u, e := ValidateInitData(valid, "token", now)
	if e != nil || u.ID != 123 {
		t.Fatal(e)
	}
	for _, raw := range []string{strings.Replace(valid, "123", "456", 1), valid + "&user=other", signed(now.Add(-25 * time.Hour)), signed(now.Add(time.Hour))} {
		if _, e = ValidateInitData(raw, "token", now); e == nil {
			t.Fatal("aceptó sesión manipulada o vencida")
		}
	}
}
