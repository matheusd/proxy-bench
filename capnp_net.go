package main

import (
	"crypto/tls"
	"log"
	"net"
	"proxy-bench/capnpnet"
	"proxy-bench/netx"
)

func init() {
	serverSessionCreators["capnp"] = func(args Args) (netx.ServerSession, error) {
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

		return capnpnet.NewServerSession(listener), nil
	}

	clientSessionCreators["capnp"] = func(args Args) (netx.ClientSession, error) {
		network, tlsEnabled, addr := splitAddress(args.Connect)
		var tlsConfig *tls.Config
		var err error

		if tlsEnabled {
			tlsConfig, err = getClientTLSConfig(args)
			if err != nil {
				return nil, err
			}
		}

		return capnpnet.NewClientSession(network, addr, tlsConfig), nil
	}
}
