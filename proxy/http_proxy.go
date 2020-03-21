package proxy

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/wweir/sower/transport"
	"github.com/wweir/utils/log"
)

// StartHTTPProxy start http reverse proxy.
// The httputil.ReverseProxy do not supply enough support for https request.
func StartHTTPProxy(httpProxyAddr, serverAddr string, password []byte, shouldProxy func(string) bool) {
	proxy := httputil.ReverseProxy{
		Director: func(r *http.Request) {},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				addr, _ = withDefaultPort(addr, "80")
				return transport.Dial(serverAddr, addr, password)
			}},
	}

	srv := &http.Server{
		Addr: httpProxyAddr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodConnect {
				httpsProxy(w, r, serverAddr, password, shouldProxy)
			} else {
				proxy.ServeHTTP(w, r)
			}
		}),
		// Disable HTTP/2.
		TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){},
		IdleTimeout:  90 * time.Second,
	}

	log.Infow("start sower http proxy", "http_proxy", httpProxyAddr)
	go log.Fatalw("serve http proxy", "addr", httpProxyAddr, "err", srv.ListenAndServe())
}

func httpsProxy(w http.ResponseWriter, r *http.Request,
	serverAddr string, password []byte, shouldProxy func(string) bool) {

	target, host := withDefaultPort(r.Host, "443")

	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	conn.(*net.TCPConn).SetKeepAlive(true)
	defer conn.Close()

	if _, err := conn.Write([]byte(r.Proto + " 200 Connection established\r\n\r\n")); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	var rc net.Conn
	if shouldProxy(host) {
		rc, err = transport.Dial(serverAddr, target, password)
	} else {
		rc, err = net.Dial("tcp", target)
	}
	if err != nil {
		conn.Write([]byte("sower dial " + serverAddr + " fail: " + err.Error()))
		conn.Close()
		return
	}
	defer rc.Close()

	relay(conn, rc)
}
