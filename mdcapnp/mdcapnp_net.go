package mdcapnp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	netx "proxy-bench/netx"

	"github.com/rs/zerolog"
	rpc "matheusd.com/mdcapnp/capnprpc"
)

var logger zerolog.Logger = /*zerolog.New(os.Stderr)*/ zerolog.Nop()

// byteStreamServer is an implementation of a capability server that provides
// the ByteStream capability. This is a low-level implementation.
type byteStreamServer struct {
	pipeReader io.ReadCloser
	pipeWriter io.WriteCloser
}

func (s *byteStreamServer) Call(ctx context.Context, cc *rpc.CallContext) error {
	if cc.InterfaceId() != ByteStream_InterfaceId {
		return fmt.Errorf("unkown interface")
	}
	switch cc.MethodId() {
	case ByteStream_Write_MethodId:
		req, err := rpc.CallContextParamsStruct[WriteRequest](cc)
		if err != nil {
			return err
		}
		_, err = s.pipeWriter.Write(req.Data())
		return err
	case ByteStream_End_MethodId:
		return s.pipeWriter.Close()
	default:
		return fmt.Errorf("unknown method")
	}
}

func newByteStreamServer() *byteStreamServer {
	pr, pw := io.Pipe()
	return &byteStreamServer{
		pipeReader: pr,
		pipeWriter: pw,
	}
}

// streamImpl is the implementation of netx.Stream. It ties one server-side
// ByteStream capability (bsServer) with one client-side ByteStream capability
// (bsClient). Read() calls read from incoming ByteStream.Write() calls, while
// Write() perform such calls.
type streamImpl struct {
	bsClient ByteStream
	bsServer *byteStreamServer
}

func (s *streamImpl) Read(p []byte) (n int, err error) {
	return s.bsServer.pipeReader.Read(p)
}

func (s *streamImpl) Write(p []byte) (n int, err error) {
	err = s.bsClient.Write(p).Wait(context.Background())
	n = len(p)
	return
}

func (s *streamImpl) Close() error {
	err1 := s.bsServer.pipeReader.Close()
	err2 := s.bsClient.End().Wait(context.Background())
	return errors.Join(err1, err2)
}

func (s *streamImpl) CloseRead() error {
	return nil // Nop; rely on Close() being called.
}

func (s *streamImpl) CloseWrite() error {
	return nil // Nop; relay on Close() being called.
}

type ServerSession struct {
	listener   net.Listener
	nextStream chan netx.Stream

	v       *rpc.Vat
	runCtx  context.Context
	stopRun func()
	runChan chan error
}

// Call implements the server side of the Proxy capability interface.
func (s *ServerSession) Call(ctx context.Context, cc *rpc.CallContext) error {
	if cc.InterfaceId() != Proxy_InterfaceId {
		return fmt.Errorf("unknown interface")
	}
	switch cc.MethodId() {
	case Proxy_OpenStream_MethodId:
		// This is the server, processing OpenStream(). 'down' is the
		// argument (cap sent by the client), 'up' is the return (cap
		// sent by the server).
		down, err := rpc.CallContextParamsCapability[ByteStream](cc)
		if err != nil {
			return fmt.Errorf("unable to get 'down' arg: %v", err)
		}
		up := newByteStreamServer()
		go func() {
			// Alert main of the next stream.
			s.nextStream <- &streamImpl{
				bsClient: down,
				bsServer: up,
			}
		}()
		return cc.RespondAsSenderHostedCap(up)

	default:
		return fmt.Errorf("unknown method")
	}
}

func (s *ServerSession) AcceptStream() (netx.Stream, error) {
	select {
	case s := <-s.nextStream:
		return s, nil
	case <-s.runCtx.Done():
		return nil, s.runCtx.Err()
	}
}

func (s *ServerSession) Close() error {
	s.stopRun()
	return <-s.runChan
}

func NewServerSession(listener net.Listener) *ServerSession {
	runChan := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	s := &ServerSession{
		listener:   listener,
		stopRun:    cancel,
		runChan:    runChan,
		runCtx:     ctx,
		nextStream: make(chan netx.Stream),
	}
	s.v = rpc.NewVat(
		rpc.WithName("server"),
		rpc.WithBootstrapHandler(s),
		rpc.WithLogger(&logger),
	)
	go func() {
		runChan <- s.v.RunWithListeners(ctx, listener)
	}()

	return s
}

type ClientSession struct {
	network   string
	address   string
	tlsConfig *tls.Config
	v         *rpc.Vat
	stopRun   func()
	runChan   chan error

	mu    sync.Mutex
	proxy Proxy
	conn  net.Conn
}

func (s *ClientSession) connect() error {
	var conn net.Conn
	var err error

	if s.tlsConfig != nil {
		conn, err = tls.Dial(s.network, s.address, s.tlsConfig)
	} else {
		conn, err = net.Dial(s.network, s.address)
	}
	if err != nil {
		return err
	}
	rv := s.v.UseRemoteVat(rpc.NewIOTransport(conn.RemoteAddr().String(), conn))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	proxy := ProxyAsRemoteVatBootstrap(rv)
	_, err = proxy.Wait(ctx)
	if err != nil {
		return err
	}
	s.proxy = proxy
	s.conn = conn
	return err
}

func (s *ClientSession) OpenStream() (netx.Stream, error) {
	var err error

	s.mu.Lock()
	if s.conn == nil {
		err = s.connect()
	}
	proxy := s.proxy
	s.mu.Unlock()

	if err != nil {
		return nil, err
	}

	down := newByteStreamServer()
	up := proxy.OpenStream(down)
	_, err = up.Wait(context.Background())
	if err != nil {
		return nil, err
	}

	return &streamImpl{
		bsClient: up,
		bsServer: down,
	}, nil
}

func (s *ClientSession) Close() error {
	s.stopRun()
	return <-s.runChan
}

func NewClientSession(network, address string, tlsConfig *tls.Config) *ClientSession {
	v := rpc.NewVat(
		rpc.WithName("client"),
		rpc.WithLogger(&logger),
	)
	runChan := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { runChan <- v.Run(ctx) }()

	return &ClientSession{
		network:   network,
		address:   address,
		tlsConfig: tlsConfig,
		stopRun:   cancel,
		v:         v,
		runChan:   runChan,
	}
}
