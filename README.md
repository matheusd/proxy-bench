# benchmark proxy transport

## Usage

### generate certificates(optional)

```bash
chmod a+x ./gen_cert.sh
./gen_cert.sh

```

### server side

```bash
iperf3 -s -p 3443
./proxy-bench -listen "capnp://127.0.0.1:2443" -connect "tcp://127.0.0.1:3443" -cert server-cert.pem -key server-key.pem
```

### client side

```bash
./proxy-bench -listen "tcp://127.0.0.1:1443" -connect "capnp://127.0.0.1:2443" -ca ca.pem
```

### benchmark

```bash
iperf3 -c 127.0.0.1 -p 1443 -t 60 -b 1G -V -l 1500
```

### Measures

CPU usage (as measured by OS):

```
$ pidstat -r -u -p <pid> 2 15
```
