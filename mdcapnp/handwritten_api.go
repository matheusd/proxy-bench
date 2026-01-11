package mdcapnp

import (
	"context"

	rpc "matheusd.com/mdcapnp/capnprpc"
	ser "matheusd.com/mdcapnp/capnpser"
)

const Proxy_InterfaceId = 0x1701c

type Proxy rpc.CallFuture

func (p Proxy) Wait(ctx context.Context) (rpc.CapabilityClient, error) {
	return rpc.WaitReturnResultsCapability[rpc.CapabilityClient](ctx, rpc.CallFuture(p))
}

func ProxyAsRemoteVatBootstrap(rv rpc.RemoteVat) Proxy {
	return Proxy(rv.Bootstrap())
}

const Proxy_OpenStream_MethodId = 0x2001

func (p Proxy) OpenStream(down rpc.CallHandler) ByteStream {
	return ByteStream(rpc.RemoteCall(
		rpc.CallFuture(p),
		rpc.SetupCallWithNewCap(rpc.CallFuture(p),
			Proxy_InterfaceId,
			Proxy_OpenStream_MethodId,
			down,
		),
	))
}

const ByteStream_InterfaceId = 0x1701d

type ByteStream rpc.CallFuture

func (bs ByteStream) Wait(ctx context.Context) (rpc.CapabilityClient, error) {
	return rpc.WaitReturnResultsCapability[rpc.CapabilityClient](ctx, rpc.CallFuture(bs))
}

var writeRequestSize = ser.StructSize{DataSectionSize: 0, PointerSectionSize: 1}

type writeRequestBuilder ser.StructBuilder

func (b *writeRequestBuilder) SetData(bytes []byte) error {
	return (*ser.StructBuilder)(b).SetData(0, bytes)
}

func newWriteRequestBuilder(serMsg *ser.MessageBuilder) (writeRequestBuilder, error) {
	return ser.NewStructBuilder[writeRequestBuilder](serMsg, writeRequestSize)
}

type WriteRequest ser.Struct

func (s *WriteRequest) Data() []byte {
	return []byte((*ser.Struct)(s).Data(0))
}

type FutureWriteResult = rpc.VoidFuture

const ByteStream_Write_MethodId = 1001

func (bs ByteStream) Write(bytes []byte) FutureWriteResult {
	vSerSize, _ := ser.ByteCount(len(bytes)).StorageWordCount()
	// vSerSize *= 4
	cs, req := rpc.SetupCallWithStructParamsGeneric[writeRequestBuilder](
		rpc.CallFuture(bs),
		writeRequestSize.TotalSize()+vSerSize, // + len(v)
		ByteStream_InterfaceId,
		ByteStream_Write_MethodId,
		writeRequestSize,
	)

	req.SetData(bytes)
	cs.WantShallowReturnCopy = true

	return FutureWriteResult(rpc.RemoteCall(
		rpc.CallFuture(bs),
		cs,
	))
}

const ByteStream_End_MethodId = 1002

func (bs ByteStream) End() rpc.VoidFuture {
	return rpc.VoidFuture(rpc.RemoteCall(
		rpc.CallFuture(bs),
		rpc.SetupCallNoParams(rpc.CallFuture(bs),
			ByteStream_InterfaceId,
			ByteStream_End_MethodId,
		),
	))
}
