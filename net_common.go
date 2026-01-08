package main

import (
	"io"
	"log"
	"net"
)

type netCommonServerSession struct {
	listener net.Listener
}

func (s *netCommonServerSession) AcceptStream() (Stream, error) {
	conn, err := s.listener.Accept()
	if err != nil {
		return nil, err
	}

	log.Printf("Accepted stream: %s->%s", conn.RemoteAddr(), conn.LocalAddr())

	return &netCommonStream{
		ReadWriteCloser: conn,
	}, nil
}

type netCommonClientSession struct {
	network string
	address string
}

func (s *netCommonClientSession) OpenStream() (Stream, error) {
	conn, err := net.Dial(s.network, s.address)
	if err != nil {
		return nil, err
	}

	log.Printf("Opened stream: %s->%s", conn.LocalAddr(), conn.RemoteAddr())

	return &netCommonStream{
		ReadWriteCloser: conn,
	}, nil
}

type netCommonStream struct {
	io.ReadWriteCloser
}

func (s *netCommonStream) CloseRead() error {
	if c, ok := s.ReadWriteCloser.(interface {
		CloseRead() error
	}); ok {
		return c.CloseRead()
	}

	return nil
}

func (s *netCommonStream) CloseWrite() error {
	if c, ok := s.ReadWriteCloser.(interface {
		CloseWrite() error
	}); ok {
		return c.CloseWrite()
	}

	return nil
}

func init() {
	netListen := func(args Args) (ServerSession, error) {
		network, addr := splitSchemeAddr(args.Listen)
		listener, err := net.Listen(network, addr)
		if err != nil {
			return nil, err
		}

		log.Printf("Listened on %s", args.Listen)

		return &netCommonServerSession{
			listener: listener,
		}, nil
	}

	netDial := func(args Args) (ClientSession, error) {
		network, addr := splitSchemeAddr(args.Connect)
		return &netCommonClientSession{
			network: network,
			address: addr,
		}, nil
	}

	for _, scheme := range []string{"tcp", "tcp4", "tcp6", "unix", "unixpacket"} {
		serverSessionCreators[scheme] = netListen
		clientSessionCreators[scheme] = netDial
	}
}
