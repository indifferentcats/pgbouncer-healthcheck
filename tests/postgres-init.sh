#!/bin/sh -e
# Build-time setup of the real Postgres behind PGBouncer in the test image.
#
# TLS is on (PGBouncer uses server_tls_sslmode = require) and clients use
# SCRAM-SHA-256. The "app" role is created with the same SCRAM secret as in
# PGBouncer's userlist.txt, so PGBouncer can pass SCRAM authentication through
# to Postgres without storing a plaintext password anywhere.

PGDATA=/var/lib/postgresql/data
APP_SCRAM=$(awk -F'"' '$2 == "app" { print $4 }' /etc/pgbouncer/userlist.txt)
[ -n "$APP_SCRAM" ] || { echo "no app user in userlist.txt" >&2; exit 1; }

install -d -o postgres -g postgres -m 700 "$PGDATA"
su postgres -c "initdb -D $PGDATA -U postgres --auth-local=trust --auth-host=scram-sha-256" >/dev/null

openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -subj /CN=localhost \
    -keyout "$PGDATA/server.key" -out "$PGDATA/server.crt" 2>/dev/null
chown postgres:postgres "$PGDATA/server.key" "$PGDATA/server.crt"
chmod 600 "$PGDATA/server.key"

cat >>"$PGDATA/postgresql.conf" <<EOF
listen_addresses = '127.0.0.1'
unix_socket_directories = '/tmp'
ssl = on
EOF

su postgres -c "pg_ctl -D $PGDATA -w -l /tmp/postgres-init.log start" >/dev/null
su postgres -c "psql -h /tmp -q -v ON_ERROR_STOP=1" <<EOF
CREATE ROLE app LOGIN PASSWORD '$APP_SCRAM';
CREATE DATABASE app OWNER app;
EOF
su postgres -c "pg_ctl -D $PGDATA -w stop" >/dev/null
