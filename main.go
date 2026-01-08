package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"proxy-bench/netx"
	"sort"
	"strings"
)

type Args struct {
	Listen   string
	Connect  string
	CertPath string
	KeyPath  string
	CAPath   string
}

func main() {
	listen := flag.String("listen", "tcp://127.0.0.1:1443", fmt.Sprintf("Listen address, available schemes: %s, add tls to enable TLS, e.g. tcp+tls", getAvailableListenSchemes()))
	connect := flag.String("connect", "tcp://127.0.0.1:", fmt.Sprintf("Connect address, available schemes: %s, add tls to enable TLS, e.g. tcp+tls", getAvailableConnectSchemes()))
	certPath := flag.String("cert", "./server-cert.pem", "Cert path for listening")
	keyPath := flag.String("key", "./server-key.pem", "Key path for listening")
	caPath := flag.String("ca", "./ca.pem", "CA cert path for connecting")
	flag.Parse()

	fmt.Println("Listen:", *listen)
	fmt.Println("Connect:", *connect)
	fmt.Println("Cert:", *certPath)
	fmt.Println("Key:", *keyPath)
	fmt.Println("CA:", *caPath)

	args := Args{
		Listen:   *listen,
		Connect:  *connect,
		CertPath: *certPath,
		KeyPath:  *keyPath,
		CAPath:   *caPath,
	}

	server := newServerSession(args)
	client := newClientSession(args)

	for {
		down, err := server.AcceptStream()
		if err != nil {
			log.Fatalf("Failed to accept downstream: %v", err)
		}

		log.Printf("Accepted new stream from client")
		go handleStream(client, down)
	}
}

func handleStream(client netx.ClientSession, down netx.Stream) {
	defer down.Close()

	up, err := client.OpenStream()
	if err != nil {
		log.Printf("Failed to open upstream: %v", err)
		return
	}

	log.Printf("Success to open new stream to server")

	defer up.Close()

	// down -> up
	go copyStream(down, up, "downstream", "upstream")

	// up -> down
	copyStream(up, down, "upstream", "downstream")
}

func copyStream(src, dst netx.Stream, srcName, dstName string) (written int64, copyErr error) {
	written, copyErr = io.Copy(dst, src)
	if copyErr != nil {
		log.Printf("Failed to copy %s -> %s(written=%d): %v", srcName, dstName, written, copyErr)
	} else {
		log.Printf("Success to copy %s -> %s(written=%d)", srcName, dstName, written)
	}

	err := src.CloseRead()
	if err != nil {
		log.Printf("Failed to close read end of %s: %v", srcName, err)
	} else {
		log.Printf("Success to close read end of %s", srcName)
	}

	err = dst.CloseWrite()
	if err != nil {
		log.Printf("Failed to close write end of %s: %v", dstName, err)
	} else {
		log.Printf("Success to close write end of %s", dstName)
	}

	return
}

type ServerSessionCreator func(args Args) (netx.ServerSession, error)

var serverSessionCreators map[string]ServerSessionCreator = make(map[string]ServerSessionCreator)

func newServerSession(args Args) netx.ServerSession {
	network, _, _ := splitAddress(args.Listen)
	creator, ok := serverSessionCreators[network]
	if !ok || creator == nil {
		log.Fatalf("Unknown scheme of listen address: %s", args.Listen)
	}

	session, err := creator(args)
	if err != nil {
		log.Fatalf("Failed to create server session: %v", err)
	}

	return session
}

func getAvailableListenSchemes() string {
	var schemes []string
	for scheme := range serverSessionCreators {
		schemes = append(schemes, scheme)
	}

	sort.Strings(schemes)
	return strings.Join(schemes, ", ")
}

type ClientSessionCreator func(args Args) (netx.ClientSession, error)

var clientSessionCreators map[string]ClientSessionCreator = make(map[string]ClientSessionCreator)

func newClientSession(args Args) netx.ClientSession {
	network, _, _ := splitAddress(args.Connect)
	creator, ok := clientSessionCreators[network]
	if !ok || creator == nil {
		log.Fatalf("Unknown scheme of connect address: %s", network)
	}

	session, err := creator(args)
	if err != nil {
		log.Fatalf("Failed to create client session: %v", err)
	}

	return session
}

func getAvailableConnectSchemes() string {
	var schemes []string
	for scheme := range clientSessionCreators {
		schemes = append(schemes, scheme)
	}

	sort.Strings(schemes)
	return strings.Join(schemes, ", ")
}

func splitAddress(s string) (network string, tls bool, addr string) {
	parts := strings.SplitN(s, "://", 2)
	if len(parts) != 2 {
		log.Panicf("Failed to split scheme and address from '%s': '://' not found", s)
	}
	addr = parts[1]

	schemes := strings.SplitN(parts[0], "+", 2)
	network = schemes[0]
	if len(schemes) == 2 && schemes[1] == "tls" {
		tls = true
	}

	return
}
