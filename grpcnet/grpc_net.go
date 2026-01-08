package grpcnet

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"proxy-bench/netx"
	sync "sync"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type ServerSession struct {
	UnimplementedProxyServer
	network   string
	address   string
	tlsConfig *tls.Config
	mu        sync.Mutex
	incoming  chan netx.Stream
	closedCh  chan struct{}
	rpcServer *grpc.Server
}

func NewServerSession(network, address string, tlsConfig *tls.Config) *ServerSession {
	return &ServerSession{
		network:   network,
		address:   address,
		tlsConfig: tlsConfig,
		incoming:  make(chan netx.Stream),
		closedCh:  make(chan struct{}),
	}
}

func (s *ServerSession) bootstrap() (<-chan netx.Stream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rpcServer != nil {
		return s.incoming, nil
	}

	listener, err := net.Listen(s.network, s.address)
	if err != nil {
		return nil, err
	}
	log.Printf("Success to listen on %s", s.address)

	var serverOpts []grpc.ServerOption
	if s.tlsConfig != nil {
		serverOpts = append(serverOpts, grpc.Creds(credentials.NewTLS(s.tlsConfig)))
	} else {
		serverOpts = append(serverOpts, grpc.Creds(insecure.NewCredentials()))
	}

	server := grpc.NewServer(serverOpts...)
	RegisterProxyServer(server, s)
	go func() {
		defer func() {
			close(s.closedCh)
		}()
		err := server.Serve(listener)
		if err != nil {
			log.Printf("gRPC Server stopped with error: %v", err)
		}
	}()

	s.rpcServer = server
	return s.incoming, nil
}

func (s *ServerSession) AcceptStream() (netx.Stream, error) {
	incoming, err := s.bootstrap()
	if err != nil {
		return nil, err
	}

	select {
	case <-s.closedCh:
		return nil, os.ErrClosed
	case stream := <-incoming:
		return stream, nil
	}
}

// OpenStream called by client
func (s *ServerSession) OpenStream(grpcStream grpc.BidiStreamingServer[Packet, Packet]) error {
	log.Printf("Recved OpenStream call from client")

	done := make(chan struct{})
	stream := newGrpcStream(grpcStream, func() {
		close(done)
	})

	select {
	case s.incoming <- stream:
	case <-s.closedCh:
		return os.ErrClosed
	}

	select {
	case <-done:
	case <-s.closedCh:
	}

	return nil
}

func (s *ServerSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.rpcServer == nil {
		return nil
	}

	s.rpcServer.Stop()
	s.rpcServer = nil
	close(s.closedCh)
	return nil
}

type ClientSession struct {
	network   string
	address   string
	tlsConfig *tls.Config
	mu        sync.Mutex
	rpcConn   *grpc.ClientConn
	rpcClient ProxyClient
}

func NewClientSession(network, address string, tlsConfig *tls.Config) *ClientSession {
	return &ClientSession{
		network:   network,
		address:   address,
		tlsConfig: tlsConfig,
	}
}

func (s *ClientSession) bootstrap() (ProxyClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rpcClient != nil {
		return s.rpcClient, nil
	}

	var dialOpts []grpc.DialOption

	if s.tlsConfig != nil {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(s.tlsConfig)))
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	grpcConn, err := grpc.NewClient(s.address, dialOpts...)
	if err != nil {
		return nil, err
	}

	s.rpcConn = grpcConn
	s.rpcClient = NewProxyClient(grpcConn)
	return s.rpcClient, nil
}

func (s *ClientSession) OpenStream() (netx.Stream, error) {
	client, err := s.bootstrap()
	if err != nil {
		return nil, err
	}

	stream, err := client.OpenStream(context.Background())
	if err != nil {
		return nil, err
	}

	return newGrpcStream(stream, nil), nil
}

func (s *ClientSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.rpcConn != nil {
		err := s.rpcConn.Close()
		s.rpcConn = nil
		s.rpcClient = nil
		return err
	}

	return nil
}

type grpcBidiStream interface {
	Recv() (*Packet, error)
	Send(*Packet) error
}

type grpcStream struct {
	stream        grpcBidiStream
	buf           bytes.Buffer
	readClosed    atomic.Bool
	writeClosed   atomic.Bool
	closeCallback func()
}

func newGrpcStream(stream grpcBidiStream, closeCallback func()) *grpcStream {
	return &grpcStream{
		stream:        stream,
		closeCallback: closeCallback,
	}
}

func (s *grpcStream) Read(p []byte) (n int, err error) {
	if s.buf.Len() > 0 {
		return s.buf.Read(p)
	}

	if s.readClosed.Load() {
		return 0, io.EOF
	}

	packet, err := s.stream.Recv()
	if err != nil {
		return 0, err
	}
	copied := copy(p, packet.Data)
	if copied == len(packet.Data) {
		return copied, nil
	}

	_, err = s.buf.Write(packet.Data[copied:])
	if err != nil {
		return 0, err
	}

	return copied, nil
}

func (s *grpcStream) Write(p []byte) (n int, err error) {
	if s.writeClosed.Load() {
		return 0, io.ErrClosedPipe
	}

	err = s.stream.Send(&Packet{
		Data: p,
	})

	if err != nil {
		return 0, err
	}

	return len(p), nil
}

func (s *grpcStream) Close() error {
	err := errors.Join(s.CloseRead(), s.CloseWrite())
	if s.closeCallback != nil {
		s.closeCallback()
	}

	return err
}

func (s *grpcStream) CloseRead() error {
	s.readClosed.Store(true)
	return nil
}

func (s *grpcStream) CloseWrite() error {
	if s.writeClosed.CompareAndSwap(false, true) {
		if cs, ok := s.stream.(interface {
			CloseSend() error
		}); ok {
			return cs.CloseSend()
		}
	}

	return nil
}
