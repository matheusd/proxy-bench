#!/bin/bash

# 生成正确的 PKI 证书结构（如果不存在）
if [ ! -f "ca.pem" ] || [ ! -f "ca-key.pem" ] || [ ! -f "server-cert.pem" ] || [ ! -f "server-key.pem" ]; then
    echo "生成 PKI 证书结构..."
    
    # 1. 生成 CA 私钥
    echo "  生成 CA 私钥..."
    openssl genrsa -out ca-key.pem 2048
    
    # 2. 生成 CA 证书
    echo "  生成 CA 证书..."
    openssl req -new -x509 -days 365 -key ca-key.pem -out ca.pem \
        -subj "/C=CN/ST=Beijing/L=Beijing/O=Test-CA/CN=Test-CA" \
        -addext "basicConstraints=critical,CA:TRUE" \
        -addext "keyUsage=critical,keyCertSign,cRLSign" \
        -addext "subjectAltName=DNS:ca.local"
    
    # 3. 创建服务器证书配置文件（如果不存在）
    if [ ! -f "server.conf" ]; then
        echo "  创建服务器证书配置文件..."
        cat > server.conf << 'EOF'
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
C = CN
ST = Beijing
L = Beijing
O = Test
CN = localhost

[v3_req]
keyUsage = keyEncipherment, digitalSignature
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
IP.1 = 127.0.0.1
EOF
    fi
    
    # 4. 生成服务器私钥
    echo "  生成服务器私钥..."
    openssl genrsa -out server-key.pem 2048
    
    # 5. 生成服务器证书签名请求
    echo "  生成服务器证书签名请求..."
    openssl req -new -key server-key.pem -out server.csr -config server.conf
    
    # 6. 使用 CA 签发服务器证书
    echo "  使用 CA 签发服务器证书..."
    openssl x509 -req -in server.csr -CA ca.pem -CAkey ca-key.pem -CAcreateserial \
        -out server-cert.pem -days 365 -extensions v3_req -extfile server.conf
    
    # 7. 清理临时文件
    rm -f server.csr
    
    echo "  PKI 证书结构生成完成！"
    echo "  文件列表："
    echo "    - ca.pem: CA 证书（用于客户端验证）"
    echo "    - ca-key.pem: CA 私钥"
    echo "    - server-cert.pem: 服务器证书"
    echo "    - server-key.pem: 服务器私钥"
fi
