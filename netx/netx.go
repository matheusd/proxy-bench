package netx

import "io"

// Stream a bidi stream
type Stream interface {
	io.ReadWriteCloser
	CloseRead() error
	CloseWrite() error
}

// ServerSession a session
type ServerSession interface {
	AcceptStream() (Stream, error)
	io.Closer
}

type ClientSession interface {
	OpenStream() (Stream, error)
	io.Closer
}
