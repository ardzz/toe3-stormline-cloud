# TOE3 Stormline Cloud

A self-hosted cloud-save server for the TOE3 Stormline mod. Each player account keeps a history of save snapshots; the
mod uploads a snapshot when the player saves and can restore any kept revision on another device.

- One server per operator: you run it, you decide who gets an account.
- PostgreSQL holds accounts, sessions and revision metadata; snapshots are stored on the filesystem or in an
  S3-compatible bucket.
- The server speaks plain HTTP and expects a reverse proxy for HTTPS. **The mod only connects over HTTPS with a
  certificate its device trusts**, such as one from Let's Encrypt; plain HTTP and self-signed certificates are refused.

## Contents

- [Quick start: Docker Compose with Caddy](#quick-start-docker-compose-with-caddy)
- [Behind an existing Traefik](#behind-an-existing-traefik)
- [Behind Cloudflare's proxy](#behind-cloudflares-proxy)
- [Other reverse proxies](#other-reverse-proxies)
- [Running the binary with systemd](#running-the-binary-with-systemd)
- [Accounts](#accounts)
- [Configuration](#configuration)
- [Storage, retention and cleanup](#storage-retention-and-cleanup)
- [Backups](#backups)
- [Upgrades](#upgrades)
- [API](#api)
- [Development](#development)

## Quick start: Docker Compose with Caddy

You need Docker with Compose v2, a domain name pointing at the machine, and ports 80 and 443 reachable from the
internet (Caddy uses them to obtain the certificate).

```sh
git clone https://github.com/ardzz/toe3-stormline-cloud.git
cd toe3-stormline-cloud/deploy
cp .env.example .env
```

Edit `.env`: set `TOE3_DOMAIN` to your domain and `POSTGRES_PASSWORD` to a generated secret of letters and digits
only, because it becomes part of a database URL (`openssl rand -hex 32`). Then start the stack and create the first
account:

```sh
docker compose up -d
docker compose exec app toe3-cloud user add alice
```

In the mod, enter `https://<TOE3_DOMAIN>` as the server URL and log in as `alice`.

The stack runs three containers: `app` (this server), `postgres` (PostgreSQL 18) and `caddy`. Snapshots live in the
`blobs` volume and the database in `pgdata`. `docker compose logs -f app` shows one line per request and the
periodic cleanup. If the image for `TOE3_VERSION` cannot be pulled, Compose builds it from this checkout instead.

Clients that reach Caddy over IPv6 show up as the Docker network's gateway, because Docker's userland proxy forwards
IPv6 connections to the IPv4-only stack network; all of them then share one rate-limit budget. IPv4 clients, and
every client behind Cloudflare, keep their own address.

## Behind an existing Traefik

If you already run Traefik with the Docker provider, publish the server through it instead of Caddy. The app does not
join Traefik's shared network: it gets a network of its own that only it and Traefik share, so no other container can
reach it directly and claim to be a proxy. Create that network and add it to Traefik's networks:

```sh
docker network create toe3-cloud-edge
```

```yaml
# in Traefik's compose file
services:
  traefik:
    networks: [default, toe3-cloud-edge]
networks:
  toe3-cloud-edge:
    external: true
```

Then, in `.env`:

```sh
COMPOSE_FILE=compose.yaml:compose.traefik.yaml
TRAEFIK_NETWORK=toe3-cloud-edge
TRAEFIK_ENTRYPOINT=websecure     # your HTTPS entrypoint
TRAEFIK_CERTRESOLVER=letsencrypt # your ACME certificate resolver
```

The app gets router and service labels for `TOE3_DOMAIN`. The default `TOE3_TRUSTED_PROXIES` covers Docker's
default address pools, which include that network; narrow or extend it if your Docker daemon uses other pools.
Keep Traefik's default handling of forwarded headers: do not set `forwardedHeaders.insecure` on the entrypoint, and
list in `forwardedHeaders.trustedIPs` only proxies that really sit in front of Traefik. Otherwise any client can set
the address the server sees.

Do not attach a `compress` middleware to this router unless it excludes `application/gzip`: snapshots are already
gzip files and are served without `Content-Encoding`, so recompressing them only wastes CPU.

## Behind Cloudflare's proxy

With the DNS record proxied (orange cloud), Cloudflare connects to Caddy on the visitor's behalf. Caddy still obtains
its certificate through Cloudflare by the HTTP-01 challenge, and Cloudflare can verify it, so set the zone's SSL/TLS
encryption mode to **Full (strict)**.

Caddy and the server must trust Cloudflare's addresses, or every player appears as one of a few Cloudflare addresses
and they share the rate limit. Put Cloudflare's published ranges into `.env`, space-separated for Caddy and appended,
comma-separated, to the server's list:

```sh
cf=$(echo $(curl -fsS https://www.cloudflare.com/ips-v4) $(curl -fsS https://www.cloudflare.com/ips-v6))
echo "CADDY_TRUSTED_PROXIES=$cf"
echo "TOE3_TRUSTED_PROXIES=172.16.0.0/12,10.0.0.0/8,192.168.0.0/16,$(echo $cf | tr ' ' ',')"
```

Copy the two printed lines into `.env` and run `docker compose up -d`. Cloudflare appends the visitor's address to
`X-Forwarded-For`, and the server takes the right-most address that is not a trusted proxy, so a visitor cannot pick
another address by sending the header themselves. Cloudflare changes these ranges rarely; refresh them when it does.

## Other reverse proxies

Any proxy that terminates HTTPS works. It must:

1. Forward `https://<your domain>/` to the server's `TOE3_LISTEN` address, keeping the path unchanged.
2. Set `X-Forwarded-For` to the client address, and be listed in `TOE3_TRUSTED_PROXIES`. The server treats the
   right-most `X-Forwarded-For` address that is not a trusted proxy as the client; that address is what the
   per-client rate limit on `/v1/auth/*` counts.
3. Allow request bodies up to `TOE3_MAX_SNAPSHOT_BYTES` (16 MiB by default). nginx needs
   `client_max_body_size 16m;`.
4. Not compress `application/gzip` responses.

Free alternatives that come with a valid certificate, handy before you own a domain: Tailscale Funnel
(`https://<machine>.<tailnet>.ts.net`) and Cloudflare Tunnel (needs a domain on Cloudflare). List the tunnel
daemon's address (usually `127.0.0.1`) in `TOE3_TRUSTED_PROXIES`; if the daemon does not send `X-Forwarded-For`,
all players share one rate-limit budget.

## Running the binary with systemd

Download `toe3-cloud_<version>_linux_<arch>` and `SHA256SUMS` from the
[releases](https://github.com/ardzz/toe3-stormline-cloud/releases), then:

```sh
sha256sum --check --ignore-missing SHA256SUMS
sudo install -m 0755 toe3-cloud_<version>_linux_<arch> /usr/local/bin/toe3-cloud
sudo install -d -m 0750 /etc/toe3-cloud
sudo install -m 0600 deploy/systemd/env.example /etc/toe3-cloud/env    # then edit it
sudo install -m 0644 deploy/systemd/toe3-cloud.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now toe3-cloud
journalctl -u toe3-cloud -f
```

The unit runs the server as a dynamic, unprivileged user with a hardened sandbox and keeps filesystem snapshots in
`/var/lib/toe3-cloud/blobs`. `TOE3_DATABASE_URL` in `/etc/toe3-cloud/env` must point at a PostgreSQL database (tested
with version 18) in which the server may create tables; migrations run automatically at start.

Put a reverse proxy on the same machine in front of `127.0.0.1:8080`. With Caddy, this Caddyfile is enough:

```caddyfile
cloud.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

## Accounts

New servers have registration **closed**: accounts are created by the operator. Set `TOE3_REGISTRATION=open` to let
players register from the mod; anyone who can reach the server can then create accounts.

| Command | Effect |
|---|---|
| `toe3-cloud user add <name>` | Create an account. |
| `toe3-cloud user passwd <name>` | Set a new password and log out all of the account's devices. |
| `toe3-cloud user disable <name>` | Block logins and log out all devices; saves are kept. |
| `toe3-cloud user enable <name>` | Allow a disabled account to log in again. |
| `toe3-cloud user delete <name>` | Delete the account, its sessions and all of its saves. |

Names are 3–32 characters of `a-z 0-9 _ . -` (case-insensitive); passwords are 10–256 characters. `add` and `passwd`
prompt for the password, or read the first line of standard input when it is not a terminal.

- **Compose:** `docker compose exec app toe3-cloud user add alice`
- **systemd:** `sudo sh -c 'set -a; . /etc/toe3-cloud/env; exec toe3-cloud user add alice'`

After five wrong passwords in a row, an account's logins are refused for 1 second, doubling with every further
failure up to 15 minutes; a successful login resets this.

## Configuration

All settings are environment variables; the binary and the container read the same ones.

| Variable | Default | Meaning |
|---|---|---|
| `TOE3_DATABASE_URL` | — (required) | PostgreSQL connection string. |
| `TOE3_LISTEN` | `127.0.0.1:8080` | Address the HTTP server listens on (the image sets `0.0.0.0:8080`). |
| `TOE3_REGISTRATION` | `closed` | `open` lets players create accounts from the mod. |
| `TOE3_TRUSTED_PROXIES` | — | Comma-separated addresses or CIDRs of your reverse proxies. |
| `TOE3_MAX_SNAPSHOT_BYTES` | `16777216` | Largest accepted snapshot, at most 67108864 (64 MiB). |
| `TOE3_RETENTION` | `20` | Revisions kept per account, 1–1000. |
| `TOE3_SESSION_TTL` | `2160h` | Login lifetime, extended by use; longer than `1h`. |
| `TOE3_BLOB_BACKEND` | `fs` | `fs` (directory) or `s3` (S3-compatible bucket). |
| `TOE3_BLOB_DIR` | `/data/blobs` | Snapshot directory for `fs`. |
| `TOE3_S3_ENDPOINT` | AWS | Service endpoint without the bucket, e.g. `https://s3.example.com`. |
| `TOE3_S3_REGION` | — | Required for `s3`. |
| `TOE3_S3_BUCKET` | — | Required for `s3`. |
| `TOE3_S3_PREFIX` | — | Optional key prefix inside the bucket. |
| `TOE3_S3_PATH_STYLE` | `false` | `true` for services that need path-style URLs (e.g. MinIO, Garage). |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | — | Required for `s3`; `AWS_SESSION_TOKEN` is optional. |

The S3 backend uses static keys only; instance profiles and other AWS credential sources are not read.

Invalid settings are all reported at once, before the server starts listening.

## Storage, retention and cleanup

- Each upload becomes a new revision. Revision numbers count up per account and are never reused.
- An upload names the revision it was based on. If the account has moved on since (another device saved), the
  server answers with a conflict, and the mod asks the player what to do instead of overwriting.
- Beyond the newest `TOE3_RETENTION` revisions, older ones are deleted.
- Snapshots that no revision references any more — pruned revisions, abandoned uploads and deleted accounts — are
  removed by a cleanup that runs at start and every 10 minutes. A snapshot is removed at least one hour after it
  became unreferenced, so deleting an account frees its storage within about 70 minutes.
- Expired sessions are purged by the same cleanup.
- On a versioned bucket (some S3 services, e.g. Backblaze B2, keep versions by default), deleted snapshots stay
  behind as old versions until a lifecycle rule removes them.

## Backups

Back up the database and the snapshots together:

1. Dump the database: `docker compose exec -T postgres pg_dump -U toe3 -Fc toe3 > toe3-$(date +%F).dump`
   (systemd: `pg_dump -Fc "$TOE3_DATABASE_URL"`).
2. Within the next hour, copy the snapshots: the `blobs` volume
   (`docker run --rm -v toe3-cloud_blobs:/data:ro -v "$PWD":/backup alpine tar czf /backup/blobs.tgz -C /data .`),
   `/var/lib/toe3-cloud/blobs`, or the bucket.

Snapshots are only deleted an hour after the database stops referencing them, so a snapshot copy taken within the
hour after the dump contains every snapshot the dump refers to. Restore both together, with the server stopped.

## Upgrades

Change `TOE3_VERSION` in `.env` (or install the new binary) and restart; the database is migrated at start. A server
refuses to start on a database migrated by a newer version, so going back to an older version means restoring a
backup taken before the upgrade.

On stop, the server finishes in-flight requests for up to 30 seconds, then cancels the rest and exits. Each request
must finish within one minute; on very slow links, snapshots near the 64 MiB maximum may not.

## API

The HTTP API is described in [`api/openapi.yaml`](api/openapi.yaml) (OpenAPI 3.1). In short:

| Endpoint | Purpose |
|---|---|
| `GET /v1/health` | Server identity, registration mode and size limit. |
| `POST /v1/auth/register`, `/login`, `/logout` | Accounts and sessions (bearer tokens). |
| `GET /v1/saves` | Revision history, newest first. |
| `PUT /v1/saves` | Upload a gzip snapshot with `If-Match: "<rev>"` or `If-None-Match: *`. |
| `GET /v1/saves/latest`, `/v1/saves/{rev}` | Download a snapshot. |

## Development

Go 1.27.3 and Docker are required.

```sh
go test ./...                              # unit tests
go test -race ./...
go test -p 1 -tags integration ./...       # PostgreSQL 18 and S3Mock via Testcontainers
npx --yes @redocly/cli@2.66.0 lint api/openapi.yaml
.github/ci/deploy-smoke.sh caddy           # the compose stack end to end (also: traefik)
```

Releases are cut by pushing a `v*` tag: CI publishes linux/amd64 and linux/arm64 binaries with `SHA256SUMS` and a
multi-platform image at `ghcr.io/ardzz/toe3-stormline-cloud`.
