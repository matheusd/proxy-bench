package main

import (
	"crypto/tls"
	"proxy-bench/grpcnet"
	"proxy-bench/netx"
)

func init() {
	serverSessionCreators["grpc"] = func(args Args) (netx.ServerSession, error) {
		_, tlsEnabled, address := splitAddress(args.Listen)
		var tlsConfig *tls.Config
		var err error
		if tlsEnabled {
			tlsConfig, err = getServerTLSConfig(args)
			if err != nil {
				return nil, err
			}
		}

		return grpcnet.NewServerSession("tcp", address, tlsConfig), nil
	}

	clientSessionCreators["grpc"] = func(args Args) (netx.ClientSession, error) {
		_, tlsEnabled, address := splitAddress(args.Connect)
		var tlsConfig *tls.Config
		var err error
		if tlsEnabled {
			tlsConfig, err = getClientTLSConfig(args)
			if err != nil {
				return nil, err
			}
		}

		return grpcnet.NewClientSession("tcp", address, tlsConfig), nil
	}
}
