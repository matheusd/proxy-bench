package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net"
	"os"
)

type stdServerSession struct {
	listener net.Listener
}

func (s *stdServerSession) AcceptStream() (Stream, error) {
	conn, err := s.listener.Accept()
	if err != nil {
		return nil, err
	}

	log.Printf("Accepted stream: %s->%s", conn.RemoteAddr(), conn.LocalAddr())

	return &stdStream{
		ReadWriteCloser: conn,
	}, nil
}

type stdClientSession struct {
	network   string
	address   string
	tlsConfig *tls.Config
}

func (s *stdClientSession) OpenStream() (Stream, error) {
	var conn net.Conn
	var err error

	if s.tlsConfig == nil {
		conn, err = net.Dial(s.network, s.address)
	} else {
		conn, err = tls.Dial(s.network, s.address, s.tlsConfig)
	}

	if err != nil {
		return nil, err
	}

	log.Printf("Opened stream: %s->%s", conn.LocalAddr(), conn.RemoteAddr())

	return &stdStream{
		ReadWriteCloser: conn,
	}, nil
}

type stdStream struct {
	io.ReadWriteCloser
}

func (s *stdStream) CloseRead() error {
	if c, ok := s.ReadWriteCloser.(interface {
		CloseRead() error
	}); ok {
		return c.CloseRead()
	}

	return nil
}

func (s *stdStream) CloseWrite() error {
	if c, ok := s.ReadWriteCloser.(interface {
		CloseWrite() error
	}); ok {
		return c.CloseWrite()
	}

	return nil
}

func getClientTLSConfig(args Args) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		NextProtos: []string{"h2"},
	}

	if args.CAPath != "" {
		caCert, err := os.ReadFile(args.CAPath)
		if err != nil {
			return nil, err
		}

		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to AppendCertsFromPEM: %s", args.CAPath)
		}

		tlsConfig.RootCAs = caPool
	}

	keylog := os.Getenv("SSLKEYLOGFILE")
	if keylog != "" {
		file, err := os.OpenFile(keylog, os.O_CREATE|os.O_RDWR, 0664)
		if err != nil {
			log.Printf("Failed to open SSLKEYLOGFILE(%s): %v", keylog, err)
		} else {
			tlsConfig.KeyLogWriter = file
			log.Printf("Success to set TLS KeyLogWriter to %s", keylog)
		}
	}

	return tlsConfig, nil
}

func getServerTLSConfig(args Args) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(args.CertPath, args.KeyPath)
	if err != nil {
		log.Fatalf("Failed to load certificate: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.NoClientCert,
		NextProtos:   []string{"h2"},
	}

	keylog := os.Getenv("SSLKEYLOGFILE")
	if keylog != "" {
		file, err := os.OpenFile(keylog, os.O_CREATE|os.O_RDWR, 0664)
		if err != nil {
			log.Printf("Failed to open SSLKEYLOGFILE(%s): %v", keylog, err)
		} else {
			tlsConfig.KeyLogWriter = file
			log.Printf("Success to set TLS KeyLogWriter to %s", keylog)
		}
	}

	return tlsConfig, nil
}

func init() {
	netListen := func(args Args) (ServerSession, error) {
		var listener net.Listener
		var err error

		network, tlsEnabled, addr := splitAddress(args.Listen)
		if tlsEnabled {
			tlsConfig, err1 := getServerTLSConfig(args)
			if err1 != nil {
				return nil, err1
			}

			listener, err = tls.Listen(network, addr, tlsConfig)
		} else {
			listener, err = net.Listen(network, addr)
		}

		if err != nil {
			return nil, err
		}

		log.Printf("Listened on %s", args.Listen)

		return &stdServerSession{
			listener: listener,
		}, nil
	}

	netDial := func(args Args) (ClientSession, error) {
		network, tlsEnabled, addr := splitAddress(args.Connect)
		sess := &stdClientSession{
			network: network,
			address: addr,
		}

		if tlsEnabled {
			tlsConfig, err := getClientTLSConfig(args)
			if err != nil {
				return nil, err
			}
			sess.tlsConfig = tlsConfig
		}

		return sess, nil
	}

	for _, scheme := range []string{"tcp", "tcp4", "tcp6", "unix", "unixpacket"} {
		serverSessionCreators[scheme] = netListen
		clientSessionCreators[scheme] = netDial
	}
}
