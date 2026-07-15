package discovery

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiscoverySOCKS5ProxyRoutesRequestsWithAuthentication(t *testing.T) {
	var targetRequests atomic.Int32
	target := newIPv4HTTPTestServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		writer.Header().Set("Content-Type", "text/html")
		_, _ = writer.Write([]byte("<html><body>proxied</body></html>"))
	}))

	proxyAddress, proxyRequests := startSOCKS5TestServer(t, "proxy user", "p@ss/word")
	client, err := newDiscoveryHTTPClient("socks5://proxy%20user:p%40ss%2Fword@"+proxyAddress, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.CloseIdleConnections)
	fetcher := HTTPClientFetcher{Client: client, Validator: allowTestURL}
	result, err := fetcher.Fetch(context.Background(), FetchRequest{URL: target.URL, Kind: FetchKindHTML})
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusOK || proxyRequests.Load() != 1 || targetRequests.Load() != 1 {
		t.Fatalf("status=%d proxy_requests=%d target_requests=%d", result.StatusCode, proxyRequests.Load(), targetRequests.Load())
	}
}

func TestDiscoverySOCKS5ProxyFailsClosed(t *testing.T) {
	for _, value := range []string{"http://127.0.0.1:1080", "socks5://127.0.0.1", "socks5://:1080", "not a url"} {
		if _, err := newDiscoveryHTTPClient(value, time.Second); !errors.Is(err, ErrInvalidSOCKS5Proxy) {
			t.Fatalf("proxy %q error = %v", value, err)
		}
	}

	var targetRequests atomic.Int32
	target := newIPv4HTTPTestServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetRequests.Add(1)
	}))
	proxyAddress, proxyRequests := startSOCKS5TestServer(t, "expected", "secret")
	client, err := newDiscoveryHTTPClient("socks5://wrong:credentials@"+proxyAddress, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.CloseIdleConnections)
	_, err = (HTTPClientFetcher{Client: client, Validator: allowTestURL}).Fetch(context.Background(), FetchRequest{URL: target.URL, Kind: FetchKindHTML})
	if err == nil {
		t.Fatal("wrong SOCKS5 credentials unexpectedly succeeded")
	}
	if proxyRequests.Load() != 0 || targetRequests.Load() != 0 {
		t.Fatalf("failed authentication leaked a request: proxy=%d target=%d", proxyRequests.Load(), targetRequests.Load())
	}
}

func startSOCKS5TestServer(t *testing.T, username string, password string) (string, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	requests := &atomic.Int32{}
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go serveSOCKS5TestConnection(connection, username, password, requests)
		}
	}()
	return listener.Addr().String(), requests
}

func newIPv4HTTPTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func serveSOCKS5TestConnection(connection net.Conn, username string, password string, requests *atomic.Int32) {
	defer connection.Close()
	reader := bufio.NewReader(connection)
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil || header[0] != 5 {
		return
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return
	}
	if _, err := connection.Write([]byte{5, 2}); err != nil {
		return
	}
	if !readSOCKS5TestCredentials(reader, connection, username, password) {
		return
	}
	request := make([]byte, 4)
	if _, err := io.ReadFull(reader, request); err != nil || request[0] != 5 || request[1] != 1 {
		return
	}
	host, ok := readSOCKS5TestHost(reader, request[3])
	if !ok {
		return
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(reader, portBytes); err != nil {
		return
	}
	upstream, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(portBytes)))), time.Second)
	if err != nil {
		_, _ = connection.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()
	if _, err := connection.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	requests.Add(1)
	done := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(upstream, reader)
		done <- struct{}{}
	}()
	_, _ = io.Copy(connection, upstream)
	<-done
}

func readSOCKS5TestCredentials(reader *bufio.Reader, connection net.Conn, username string, password string) bool {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil || header[0] != 1 {
		return false
	}
	user := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, user); err != nil {
		return false
	}
	length, err := reader.ReadByte()
	if err != nil {
		return false
	}
	pass := make([]byte, int(length))
	if _, err := io.ReadFull(reader, pass); err != nil {
		return false
	}
	status := byte(0)
	if string(user) != username || string(pass) != password {
		status = 1
	}
	_, _ = connection.Write([]byte{1, status})
	return status == 0
}

func readSOCKS5TestHost(reader *bufio.Reader, addressType byte) (string, bool) {
	switch addressType {
	case 1:
		value := make([]byte, net.IPv4len)
		_, err := io.ReadFull(reader, value)
		return net.IP(value).String(), err == nil
	case 3:
		length, err := reader.ReadByte()
		if err != nil {
			return "", false
		}
		value := make([]byte, int(length))
		_, err = io.ReadFull(reader, value)
		return string(value), err == nil
	case 4:
		value := make([]byte, net.IPv6len)
		_, err := io.ReadFull(reader, value)
		return net.IP(value).String(), err == nil
	default:
		return "", false
	}
}
