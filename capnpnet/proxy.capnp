using Go = import "/go.capnp";

@0x89a1f516c2d6455d;
$Go.package("capnpnet");
$Go.import("proxy-bench/capnpnet");

interface Proxy {
  dial @0 (down :ByteStream) -> (up :ByteStream);

  interface ByteStream {
    write @0 (bytes: Data) -> stream;
    # Write a chunk.
    end @1 ();
    # Signals clean EOF. (If the ByteStream is dropped without calling this, then the stream was
    # prematurely canceled and so the body should not be considered complete.)
  }
}
