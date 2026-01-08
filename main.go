package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"strings"
)

func main() {
	listen := flag.String("listen", "tcp://127.0.0.1:1443", "Listen address, available schemes: unix://, tcp://, tls://, grpc://, capnp://")
	connect := flag.String("connect", "tcp://127.0.0.1:", "Connect address, available schemes: unix://, tcp://, tls://, grpc://, capnp://")
	certPath := flag.String("cert", "./server.crt", "Cert path for listening")
	keyPath := flag.String("key", "./server.key", "Key path for listening")
	caPath := flag.String("ca", "./ca.crt", "CA cert path for connecting")

	flag.Parse()
	fmt.Println("Listen:", *listen)
	fmt.Println("Connect:", *connect)
	fmt.Println("Cert:", *certPath)
	fmt.Println("Key:", *keyPath)
	fmt.Println("CA:", *caPath)

	server := newServerSession(*listen, *certPath, *keyPath)
	client := newClientSession(*connect, *caPath)

	for {
		down, err := server.AcceptStream()
		if err != nil {
			log.Fatalf("Failed to accept downstream: %v", err)
		}

		go handleStream(client, down)
	}
}

func handleStream(client ClientSession, down Stream) {
	defer down.Close()

	up, err := client.OpenStream()
	if err != nil {
		log.Printf("Failed to open upstream: %v", err)
	}

	defer up.Close()

	// down -> up
	go copyStream(down, up, "downstream", "upstream")

	// up -> down
	copyStream(up, down, "upstream", "downstream")
}

func copyStream(src, dst Stream, srcName, dstName string) (written int64, copyErr error) {
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

func newServerSession(listen, certPart, keyPath string) ServerSession {
	return nil
}

func newClientSession(connect, caPath string) ClientSession {
	return nil
}

func splitSchemeAddr(s string) (scheme, addr string) {
	parts := strings.SplitN(s, "://", 2)
	if len(parts) != 2 {
		log.Panicf("Failed to split scheme and address from '%s': '://' not found", s)
	}

	return parts[0], parts[1]
}

// Stream a bidi stream
type Stream interface {
	io.ReadWriteCloser
	CloseRead() error
	CloseWrite() error
}

// ServerSession a session
type ServerSession interface {
	AcceptStream() (Stream, error)
}

type ClientSession interface {
	OpenStream() (Stream, error)
}
