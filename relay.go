package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	dialTimeout   = 10 * time.Second
	warmTTL       = 25 * time.Second
	warmMaxAge    = 20 * time.Second
	warmPerKey    = 10
	maxConns      = 16384
	clientIdle    = 60 * time.Second
	keepAliveTime = 30 * time.Second
)

// Shifter upstream addresses (DigitalOcean NYC first, then São Paulo fallback).
var shifterAddrs = []string{"165.227.155.232:443", "201.79.5.23:443"}

// Preferred (last successful) Shifter address index.
var (
	prefMu  sync.Mutex
	prefIdx = 0
)

func orderedAddrs() []string {
	prefMu.Lock()
	defer prefMu.Unlock()
	addrs := make([]string, len(shifterAddrs))
	copy(addrs, shifterAddrs)
	addrs[0], addrs[prefIdx] = addrs[prefIdx], addrs[0]
	return addrs
}

func markPreferred(addr string) {
	prefMu.Lock()
	defer prefMu.Unlock()
	for i, a := range shifterAddrs {
		if a == addr {
			prefIdx = i
			return
		}
	}
}

// connAlive checks liveness of a TCP conn without consuming buffered data,
// using a non-blocking MSG_PEEK recv. Returns true if the peer is still
// connected (EAGAIN = open, no data) and false on EOF (peer closed).
func connAlive(c *net.TCPConn) bool {
	raw, err := c.SyscallConn()
	if err != nil {
		return false
	}
	alive := false
	err = raw.Read(func(fd uintptr) bool {
		var buf [1]byte
		n, _, errno := syscall.Recvfrom(int(fd), buf[:], syscall.MSG_PEEK|syscall.MSG_DONTWAIT)
		if errno == syscall.EAGAIN || errno == syscall.EWOULDBLOCK {
			alive = true
			return true
		}
		if errno == nil && n == 0 {
			alive = false
			return true
		}
		alive = true
		return true
	})
	if err != nil {
		return false
	}
	return alive
}

type warmEntry struct {
	c  *net.TCPConn
	at time.Time
}

type warmPool struct {
	mu sync.Mutex
	m  map[string][]*warmEntry
}

var pool = &warmPool{m: make(map[string][]*warmEntry)}

// take returns a live warm tunnel for key, or nil. Dead or too-old tunnels
// are closed and removed; tunnels older than warmMaxAge are rejected because
// residential exits can silently drop them (NAT churn) while the gateway TCP
// stays alive, which would surface as SSLHandshake errors on reuse.
func (p *warmPool) take(key string) *net.TCPConn {
	p.mu.Lock()
	defer p.mu.Unlock()
	arr := p.m[key]
	for len(arr) > 0 {
		last := len(arr) - 1
		e := arr[last]
		arr = arr[:last]
		if e != nil && e.c != nil && time.Since(e.at) < warmMaxAge && connAlive(e.c) {
			p.m[key] = arr
			return e.c
		}
		if e != nil && e.c != nil {
			e.c.Close()
		}
	}
	p.m[key] = arr
	return nil
}

func (p *warmPool) push(key string, c *net.TCPConn) {
	p.mu.Lock()
	arr := p.m[key]
	if len(arr) >= warmPerKey {
		p.mu.Unlock()
		c.Close()
		return
	}
	p.m[key] = append(arr, &warmEntry{c: c, at: time.Now()})
	p.mu.Unlock()

	go func() {
		time.Sleep(warmTTL)
		p.mu.Lock()
		a := p.m[key]
		for i, x := range a {
			if x != nil && x.c == c {
				p.m[key] = append(a[:i], a[i+1:]...)
				break
			}
		}
		p.mu.Unlock()
		c.Close()
	}()
}

// shifterConnect dials Shifter (trying each address) and completes the CONNECT
// handshake. Returns the ready connection plus a bufio.Reader that may hold
// leftover bytes already received from the tunnel.
func shifterConnect(target, auth string) (*net.TCPConn, *bufio.Reader, error) {
	var lastErr error
	for _, addr := range orderedAddrs() {
		d := net.Dialer{Timeout: dialTimeout, KeepAlive: keepAliveTime}
		conn, err := d.Dial("tcp", addr)
		if err != nil {
			lastErr = err
			continue
		}
		tc := conn.(*net.TCPConn)
		tc.SetNoDelay(true)
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(keepAliveTime)

		req := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n"
		if auth != "" {
			req += "Proxy-Authorization: " + auth + "\r\n"
		}
		req += "\r\n"

		tc.SetDeadline(time.Now().Add(dialTimeout))
		if _, err := tc.Write([]byte(req)); err != nil {
			tc.Close()
			lastErr = err
			continue
		}

		br := bufio.NewReaderSize(tc, 4096)
		statusLine, err := br.ReadString('\n')
		if err != nil {
			tc.Close()
			lastErr = err
			continue
		}
		ok := false
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				break
			}
			if strings.TrimSpace(line) == "" {
				ok = true
				break
			}
		}
		if !ok || !strings.Contains(statusLine, " 2") {
			tc.Close()
			lastErr = fmt.Errorf("bad status %q", statusLine)
			continue
		}
		tc.SetDeadline(time.Time{})
		markPreferred(addr)
		return tc, br, nil
	}
	return nil, nil, lastErr
}

// drainBuffered writes any bytes already buffered in br to dst.
func drainBuffered(br *bufio.Reader, dst net.Conn) {
	if br == nil {
		return
	}
	if n := br.Buffered(); n > 0 {
		if b, err := br.Peek(n); err == nil {
			dst.Write(b)
		}
	}
}

func poolWarm(key, target, auth string) {
	tc, br, err := shifterConnect(target, auth)
	if err != nil {
		return
	}
	// If bytes already arrived, the tunnel is not clean; discard it.
	if br.Buffered() > 0 {
		tc.Close()
		return
	}
	pool.push(key, tc)
}

// Common Google AdMob hosts the SDK (WebView) fetches during an ad load.
var commonHosts = []string{
	"googleads.g.doubleclick.net:443",
	"tpc.googlesyndication.com:443",
	"pagead2.googlesyndication.com:443",
	"www.gstatic.com:443",
	"www.googletagservices.com:443",
	"pagead2.googleadservices.com:443",
	"adservice.google.com:443",
	"securepubads.g.doubleclick.net:443",
}

// seenAuths prevents repeated broad pre-warming for the same session auth.
var (
	seenMu    sync.Mutex
	seenAuths = make(map[string]bool)
)

// maybeWarmAllHosts pre-warms every common AdMob host for a freshly seen
// session auth, so the SDK's parallel requests skip the CONNECT handshake.
func maybeWarmAllHosts(auth string) {
	if auth == "" {
		return
	}
	seenMu.Lock()
	if seenAuths[auth] {
		seenMu.Unlock()
		return
	}
	seenAuths[auth] = true
	if len(seenAuths) > 500 {
		seenAuths = make(map[string]bool)
	}
	seenMu.Unlock()
	for _, h := range commonHosts {
		go poolWarm(auth+"\x00"+h, h, auth)
	}
}

func handleConn(client net.Conn) {
	defer client.Close()
	tc, ok := client.(*net.TCPConn)
	if !ok {
		return
	}
	tc.SetNoDelay(true)
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(keepAliveTime)

	// Idle timeout while waiting for the CONNECT request.
	client.SetDeadline(time.Now().Add(clientIdle))

	br := bufio.NewReaderSize(client, 4096)
	reqLine, err := br.ReadString('\n')
	if err != nil {
		return
	}
	parts := strings.SplitN(strings.TrimSpace(reqLine), " ", 3)
	if len(parts) < 3 || parts[0] != "CONNECT" {
		// Not a tunnel request (e.g. a browser probing the port). Answer a
		// friendly JSON so nothing about the relay leaks and it keeps running.
		respondJsonOk(client, br)
		return
	}
	target := parts[1]

	auth := ""
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "proxy-authorization:") {
			auth = strings.TrimSpace(line[len("Proxy-Authorization:"):])
		}
	}

	// Clear the idle deadline now that the tunnel is established.
	client.SetDeadline(time.Time{})

	key := auth + "\x00" + target

	// Broadly pre-warm all common AdMob hosts for this freshly seen session.
	maybeWarmAllHosts(auth)

	var up *net.TCPConn
	var upBr *bufio.Reader
	if up = pool.take(key); up == nil {
		up, upBr, err = shifterConnect(target, auth)
		if err != nil {
			log.Printf("shifter-connect-failed %s: %v", target, err)
			client.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n"))
			return
		}
		log.Printf("warm-miss %s", target)
	} else {
		log.Printf("warm-hit %s", target)
	}
	defer up.Close()

	client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// Pre-warm the next tunnel for this (auth,target) in the background.
	go poolWarm(key, target, auth)

	// Forward any upstream bytes that already arrived, then the client's
	// buffered TLS ClientHello.
	drainBuffered(upBr, client)
	drainBuffered(br, up)

	// Zero-copy bidirectional forwarding (io.Copy uses splice(2) on Linux
	// when both endpoints are *net.TCPConn).
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(up, client)
		up.CloseWrite()
		done <- struct{}{}
	}()
	go func() {
		io.Copy(client, up)
		done <- struct{}{}
	}()
	<-done
}

var connSem = make(chan struct{}, maxConns)

// respondJsonOk answers non-tunnel HTTP requests (e.g. a browser hitting the
// relay port) with a small JSON body so the relay reveals nothing and keeps
// running normally.
func respondJsonOk(client net.Conn, br *bufio.Reader) {
	// Drain the rest of the (small) request so the client can read our reply.
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			break
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}
	body := "{\"status\":\"ok\"}\n"
	client.SetWriteDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprintf(client, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
}

func main() {
	addr := ":8071"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("relay listening on %s -> %v (maxConns=%d)", addr, shifterAddrs, maxConns)

	// Graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("shutting down...")
		ln.Close()
		os.Exit(0)
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			return
		}
		connSem <- struct{}{}
		go func(c net.Conn) {
			defer func() { <-connSem }()
			handleConn(c)
		}(conn)
	}
}
