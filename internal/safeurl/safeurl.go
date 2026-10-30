// Package safeurl pins DNS resolution to a public address, including redirects.
package safeurl

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("2001::/32"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
}

func Public(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	for _, p := range reserved {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}
func Parse(raw string) (*url.URL, error) {
	if len(raw) > 4096 {
		return nil, fmt.Errorf("enlace demasiado largo")
	}
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil {
		return nil, fmt.Errorf("URL inválida")
	}
	if (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || strings.ContainsAny(raw, "\r\n\x00") {
		return nil, fmt.Errorf("usa un enlace HTTP o HTTPS público sin credenciales")
	}
	if u.Port() != "" && u.Port() != "80" && u.Port() != "443" {
		return nil, fmt.Errorf("solo puertos 80 y 443")
	}
	return u, nil
}
func Resolve(ctx context.Context, host string) (netip.Addr, error) {
	ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if e != nil || len(ips) == 0 {
		return netip.Addr{}, fmt.Errorf("no se pudo resolver el dominio")
	}
	for _, ip := range ips {
		if !Public(ip) {
			return netip.Addr{}, fmt.Errorf("no se permiten redes privadas o reservadas")
		}
	}
	return ips[0], nil
}
func Validate(ctx context.Context, raw string) error {
	u, e := Parse(raw)
	if e != nil {
		return e
	}
	_, e = Resolve(ctx, u.Hostname())
	return e
}
func Dial(ctx context.Context, network, address string) (net.Conn, error) {
	h, p, e := net.SplitHostPort(address)
	if e != nil {
		return nil, e
	}
	if p != "80" && p != "443" {
		return nil, fmt.Errorf("puerto no permitido")
	}
	ip, e := Resolve(ctx, h)
	if e != nil {
		return nil, e
	}
	d := net.Dialer{Timeout: 15 * time.Second}
	return d.DialContext(ctx, network, net.JoinHostPort(ip.String(), p))
}
func Client() *http.Client {
	return &http.Client{Timeout: 0, Transport: &http.Transport{Proxy: nil, DialContext: Dial, TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second}, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 8 {
			return fmt.Errorf("demasiadas redirecciones")
		}
		_, e := Parse(r.URL.String())
		return e
	}}
}

// Proxy applies the same egress policy to yt-dlp and gallery-dl subprocesses.
type Proxy struct{ client *http.Client }

func NewProxy() *Proxy { return &Proxy{client: Client()} }
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		conn, e := Dial(ctx, "tcp", r.Host)
		cancel()
		if e != nil {
			http.Error(w, "destino bloqueado", 403)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			conn.Close()
			http.Error(w, "tunnel no disponible", 500)
			return
		}
		client, buf, e := hijacker.Hijack()
		if e != nil {
			conn.Close()
			return
		}
		client.SetDeadline(time.Now().Add(2 * time.Hour))
		conn.SetDeadline(time.Now().Add(2 * time.Hour))
		buf.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		buf.Flush()
		go func() {
			defer client.Close()
			defer conn.Close()
			done := make(chan struct{})
			go func() {
				io.Copy(conn, buf)
				if c, ok := conn.(*net.TCPConn); ok {
					c.CloseWrite()
				}
				close(done)
			}()
			io.Copy(client, conn)
			client.Close()
			conn.Close()
			<-done
		}()
		return
	}
	if _, e := Parse(r.URL.String()); e != nil {
		http.Error(w, "destino inválido", 400)
		return
	}
	req := r.Clone(r.Context())
	req.RequestURI = ""
	req.Header.Del("Proxy-Authorization")
	req.Header.Del("Proxy-Connection")
	req.Header.Del("Connection")
	resp, e := p.client.Do(req)
	if e != nil {
		http.Error(w, "no se pudo conectar al destino", 502)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		if strings.EqualFold(k, "Connection") {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
