#!/usr/bin/env bash
set -euo pipefail

# Output folder for generated CA/server/client cert material.
OUT=${1:-certs}
mkdir -p "$OUT"

# Create root CA used to sign relay and client certificates.
echo "Generating CA..."
openssl genpkey -algorithm RSA -out "$OUT/ca.key" -pkeyopt rsa_keygen_bits:2048
openssl req -x509 -new -nodes -key "$OUT/ca.key" -sha256 -days 3650 -subj "/CN=bigbrother-CA" -out "$OUT/ca.crt"

# Create relay/server keypair and signing request.
echo "Generating server key/csr..."
openssl genpkey -algorithm RSA -out "$OUT/server.key" -pkeyopt rsa_keygen_bits:2048
openssl req -new -key "$OUT/server.key" -subj "/CN=localhost" -out "$OUT/server.csr"

# Include localhost SANs for local development and loopback testing.
cat > "$OUT/server_ext.cnf" <<EOF
subjectAltName = DNS:localhost,IP:127.0.0.1
EOF

openssl x509 -req -in "$OUT/server.csr" -CA "$OUT/ca.crt" -CAkey "$OUT/ca.key" -CAcreateserial -out "$OUT/server.crt" -days 365 -sha256 -extfile "$OUT/server_ext.cnf"

# Create client cert used by agents/controllers in mTLS setups.
echo "Generating client key/csr..."
openssl genpkey -algorithm RSA -out "$OUT/client.key" -pkeyopt rsa_keygen_bits:2048
openssl req -new -key "$OUT/client.key" -subj "/CN=agent-client" -out "$OUT/client.csr"
openssl x509 -req -in "$OUT/client.csr" -CA "$OUT/ca.crt" -CAkey "$OUT/ca.key" -CAcreateserial -out "$OUT/client.crt" -days 365 -sha256

echo "Wrote certs to $OUT"
ls -l "$OUT"
