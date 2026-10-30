#!/usr/bin/env python3
"""Print a PostgreSQL/PGBouncer SCRAM-SHA-256 secret for tests/userlist.txt.

Usage: tests/scram-secret.py PASSWORD [ITERATIONS]

Output format (RFC 5802 / RFC 7677, as stored by PostgreSQL):
    SCRAM-SHA-256$<iterations>:<salt>$<StoredKey>:<ServerKey>
"""
import base64
import hashlib
import hmac
import os
import sys


def scram_secret(password: str, iterations: int = 4096) -> str:
    salt = os.urandom(16)
    salted = hashlib.pbkdf2_hmac("sha256", password.encode(), salt, iterations)
    client_key = hmac.new(salted, b"Client Key", hashlib.sha256).digest()
    stored_key = hashlib.sha256(client_key).digest()
    server_key = hmac.new(salted, b"Server Key", hashlib.sha256).digest()
    b64 = lambda b: base64.b64encode(b).decode()
    return f"SCRAM-SHA-256${iterations}:{b64(salt)}${b64(stored_key)}:{b64(server_key)}"


if __name__ == "__main__":
    if len(sys.argv) not in (2, 3):
        sys.exit(__doc__)
    print(scram_secret(sys.argv[1], int(sys.argv[2]) if len(sys.argv) == 3 else 4096))
