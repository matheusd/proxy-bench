package capnpnet

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"os"
	netx "proxy-bench/netx"
	"sync"
	"sync/atomic"
	"time"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/flowcontrol"
	"capnproto.org/go/capnp/v3/rpc"
)

type CapnpServerSession struct {
	listener net.Listener
	mu       sync.Mutex
	incoming chan netx.Stream
	rpcConn  *rpc.Conn
}

func NewServerSession(listener net.Listener) *CapnpServerSession {
	return &CapnpServerSession{
		listener: listener,
		incoming: make(chan netx.Stream),
	}
}

func (s *CapnpServerSession) bootstrap() (incoming <-chan netx.Stream, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rpcConn != nil {
		return s.incoming, nil
	}

	conn, err := s.listener.Accept()
	if err != nil {
		return nil, err
	}

	rpcServer := Proxy_ServerToClient(s)
	s.rpcConn = rpc.NewConn(rpc.NewStreamTransport(conn), &rpc.Options{
		BootstrapClient: capnp.Client(rpcServer),
	})

	s.incoming = make(chan netx.Stream)
	return s.incoming, nil
}

func (s *CapnpServerSession) AcceptStream() (netx.Stream, error) {
	incoming, err := s.bootstrap()
	if err != nil {
		return nil, err
	}

	stream, ok := <-incoming
	if !ok {
		return nil, os.ErrClosed
	}

	return stream, nil
}

func (s *CapnpServerSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.rpcConn != nil {
		s.rpcConn.Close()
	}

	s.listener.Close()
	return nil
}

// Dial called by client
func (s *CapnpServerSession) Dial(ctx context.Context, call Proxy_dial) error {
	res, err := call.AllocResults()
	if err != nil {
		return err
	}

	up := newByteStreamReader()
	err = res.SetUp(Proxy_ByteStream_ServerToClient(up))
	if err != nil {
		return err
	}

	down := call.Args().Down().AddRef()
	s.incoming <- newCapnpStream(up, down)
	return nil
}

type CapnpClientSession struct {
	network     string
	address     string
	tlsConfig   *tls.Config
	mu          sync.Mutex
	rpcConn     *rpc.Conn
	proxyClient Proxy
}

func NewClientSession(network, address string, tlsConfig *tls.Config) *CapnpClientSession {
	return &CapnpClientSession{
		network:   network,
		address:   address,
		tlsConfig: tlsConfig,
	}
}

func (s *CapnpClientSession) bootstrap() (Proxy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.rpcConn != nil {
		return s.proxyClient, nil
	}

	var conn net.Conn
	var err error

	if s.tlsConfig != nil {
		conn, err = tls.Dial(s.network, s.address, s.tlsConfig)
	} else {
		conn, err = net.Dial(s.network, s.address)
	}
	if err != nil {
		return Proxy{}, err
	}

	s.rpcConn = rpc.NewConn(rpc.NewStreamTransport(conn), nil)
	s.proxyClient = Proxy(s.rpcConn.Bootstrap(context.Background()))
	return s.proxyClient, nil
}

func (s *CapnpClientSession) OpenStream() (netx.Stream, error) {
	proxy, err := s.bootstrap()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()

	reader := newByteStreamReader()
	downStream := Proxy_ByteStream_ServerToClient(reader)
	downStream.SetFlowLimiter(flowcontrol.NewFixedLimiter(1024 * 1024 * 4))
	future, release := proxy.Dial(ctx, func(p Proxy_dial_Params) error {
		return p.SetDown(downStream)
	})

	res, err := future.Struct()
	if err != nil {
		release()
		return nil, err
	}

	reader.release = release
	writer := res.Up()
	return newCapnpStream(reader, writer), nil
}

func (s *CapnpClientSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rpcConn == nil {
		return nil
	}

	rpcConn := s.rpcConn
	s.rpcConn = nil
	s.proxyClient = Proxy{}
	return rpcConn.Close()
}

type capnpStream struct {
	reader *byteStreamReader
	writer Proxy_ByteStream
	closed atomic.Bool
}

func newCapnpStream(reader *byteStreamReader, writer Proxy_ByteStream) *capnpStream {
	return &capnpStream{
		reader: reader,
		writer: writer,
	}
}

func (s *capnpStream) Read(p []byte) (n int, err error) {
	return s.reader.Read(p)
}

func (s *capnpStream) Write(b []byte) (n int, err error) {
	if s.closed.Load() {
		return 0, io.ErrClosedPipe
	}

	err = s.writer.Write(context.Background(), func(p Proxy_ByteStream_write_Params) error {
		return p.SetBytes(b)
	})
	if err != nil {
		return 0, err
	}

	return len(b), nil
}

func (s *capnpStream) Close() error {
	s.CloseWrite()
	return nil
}

func (s *capnpStream) CloseRead() error {
	// do nothing
	return nil
}

func (s *capnpStream) CloseWrite() error {
	if s.closed.CompareAndSwap(false, true) {
		future, release := s.writer.End(context.Background(), func(p Proxy_ByteStream_end_Params) error {
			return nil
		})
		defer release()

		_, err := future.Ptr()
		return err
	}

	return nil
}

type byteStreamReader struct {
	reader  *io.PipeReader
	writer  *io.PipeWriter
	release capnp.ReleaseFunc
}

func newByteStreamReader() *byteStreamReader {
	reader, writer := io.Pipe()

	return &byteStreamReader{
		reader: reader,
		writer: writer,
	}
}

// Write called by peer
func (s *byteStreamReader) Write(ctx context.Context, call Proxy_ByteStream_write) error {
	bytes, err := call.Args().Bytes()
	if err != nil {
		return err
	}

	_, err = s.writer.Write(bytes)
	return err

}

// Read call by this side
func (s *byteStreamReader) Read(p []byte) (n int, err error) {
	return s.reader.Read(p)
}

func (s *byteStreamReader) Close() error {
	s.release()
	return nil
}

// End called by peer
func (s *byteStreamReader) End(ctx context.Context, call Proxy_ByteStream_end) error {
	// close writer end of pipe
	s.writer.Close()
	return nil
}
