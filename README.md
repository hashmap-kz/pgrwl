# pgrwl

> Stream PostgreSQL WALs with Zero Data Loss

[![License](https://img.shields.io/github/license/hashmap-kz/pgrwl)](https://github.com/hashmap-kz/pgrwl/blob/master/LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/hashmap-kz/pgrwl)](https://goreportcard.com/report/github.com/hashmap-kz/pgrwl)
[![Go Reference](https://pkg.go.dev/badge/github.com/hashmap-kz/pgrwl.svg)](https://pkg.go.dev/github.com/hashmap-kz/pgrwl)
[![Workflow Status](https://img.shields.io/github/actions/workflow/status/hashmap-kz/pgrwl/ci.yml?branch=master)](https://github.com/hashmap-kz/pgrwl/actions/workflows/ci.yml?query=branch:master)
[![GitHub Issues](https://img.shields.io/github/issues/hashmap-kz/pgrwl)](https://github.com/hashmap-kz/pgrwl/issues)
[![Go Version](https://img.shields.io/github/go-mod/go-version/hashmap-kz/pgrwl)](https://github.com/hashmap-kz/pgrwl/blob/master/go.mod#L3)
[![Latest Release](https://img.shields.io/github/v/release/hashmap-kz/pgrwl)](https://github.com/hashmap-kz/pgrwl/releases/latest)
[![Start contributing](https://img.shields.io/github/issues/hashmap-kz/pgrwl/good%20first%20issue?color=7057ff&label=Contribute)](https://github.com/hashmap-kz/pgrwl/issues?q=is%3Aissue+is%3Aopen+sort%3Aupdated-desc+label%3A%22good+first+issue%22)

**pgrwl** is a PostgreSQL write-ahead log (WAL) receiver written in Go. It’s a drop-in, container-friendly alternative
to `pg_receivewal`, supporting streaming replication, encryption, compression, and remote storage (S3, SFTP).

Designed for disaster recovery and PITR (Point-in-Time Recovery), `pgrwl` ensures zero data loss (RPO=0) and seamless
integration with Kubernetes environments.

---

## Table of Contents

- [About](#about)
- [Usage](#usage)
    - [Receive Mode](#receive-mode-quick-start)
    - [Serve Mode](#serve-mode)
    - [Backup Mode](#backup-mode)
    - [Restore Command](#restore-command)
- [Quick Start](examples)
    - [Docker Compose (Basic Setup)](examples/docker-compose-quick-start/)
    - [Docker Compose (Archive And Recovery)](examples/docker-compose-recovery-example/)
    - [Kubernetes (all features: s3-storage, compression, encryption, retention, monitoring, etc...)](examples/k8s-quick-start/)
- [Configuration Reference](#configuration-reference)
- [Installation](#installation)
    - [Docker Images](#docker-images-are-available-at-quayiohashmap_kzpgrwl)
    - [Binaries](#manual-installation)
    - [Packages](#package-based-installation-suitable-in-cicd)
    - [Helm Chart](#helm-chart)
- [Disaster Recovery Use Cases](#disaster-recovery-use-cases)
- [Architecture](#architecture)
    - [Design Notes](#design-notes)
    - [Durability & `fsync`](#durability--fsync)
    - [Why Not archive_command `archive_command`?](#why-not-archive_command)
- [Contributing](#contributing)
- [License](#license)

---

## About

- The project serves as a **research platform** to explore streaming WAL archiving with a target of **RPO=0** during
  recovery.

- _It's primarily designed for use in containerized environments._

- The utility replicates all key features of `pg_receivewal`, including automatic reconnection on connection loss,
  streaming into partial files, extensive error checking and more.

- Install as a single binary. Debug with your favorite editor and a local PostgreSQL
  container ([local-dev-infra](test/integration/environ/)).

**`pgrwl` running in `receive` mode**

![Receive Mode](https://github.com/hashmap-kz/assets/blob/main/pgrwl/loop-v1.png)

**`pgrwl` running in `serve` mode**

![Serve Mode](https://github.com/hashmap-kz/assets/blob/main/pgrwl/serve-mode.png)

**`pgrwl` running in `backup` mode**

![Serve Mode](https://github.com/hashmap-kz/assets/blob/main/pgrwl/backup-mode.png)

---

## Usage

**[`^        back to top        ^`](#table-of-contents)**

### Receive Mode Quick Start

`Receive` mode is _the main loop of the WAL receiver_.

```bash
#!/usr/bin/env bash
set -euo pipefail

# Clone repo
git clone https://github.com/hashmap-kz/pgrwl.git
cd pgrwl

# Prepare docker-compose files
cat <<EOF >pg_hba.conf
local all         all     trust
local replication all     trust
host  all         all all trust
host  replication all all trust
EOF

cat <<EOF >docker-compose.yml
services:
  pg-primary:
    image: postgres:17
    container_name: pg-primary
    restart: unless-stopped
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
    ports:
      - "15432:5432"
    volumes:
      - pg-primary-data:/var/lib/postgresql/17/main
      - ./pg_hba.conf:/etc/postgresql/pg_hba.conf:ro
    command: -c wal_level=replica
      -c max_wal_senders=10
      -c wal_keep_size=64MB
      -c listen_addresses=*
      -c log_replication_commands=on
      -c hba_file=/etc/postgresql/pg_hba.conf
volumes:
  pg-primary-data:
EOF

# Run containers, wait while pg is ready
docker compose up -d
until docker exec pg-primary pg_isready -U postgres >/dev/null 2>&1; do
  echo "Waiting for PostgreSQL to be ready..."
  sleep 1
done

# Run WAL receiver with local storage
cat <<EOF >config.yml
main:
  listen_port: 7070
  directory: wals
receiver:
  slot: pgrwl_v5
log:
  level: trace
  format: text
  add_source: true
EOF

export PGHOST=localhost
export PGPORT=15432
export PGUSER=postgres
export PGPASSWORD=postgres
export PGRWL_MODE=receive
go run main.go start -c config.yml
```

### Serve Mode

`Serve` mode is _used during restore to serve archived WAL files from storage_.

```bash
cat <<EOF >config.yml
main:
  listen_port: 7070
  directory: wals
log:
  level: trace
  format: text
  add_source: true
EOF

export PGRWL_MODE=serve

pgrwl start -c config.yml
```

### Backup Mode

`Backup` mode performs a full base backup of your PostgreSQL cluster on a configured schedule.

```bash
cat <<EOF >config.yml
main:
  listen_port: 7070
  directory: wals
backup:
  cron: "0 0 */3 * *"
  retention:
    enable: true
    type: time
    value: "96h"
log:
  level: trace
  format: text
  add_source: true
EOF

export PGHOST=localhost
export PGPORT=5432
export PGUSER=postgres
export PGPASSWORD=postgres
export PGRWL_MODE=backup

pgrwl start -c config.yml
```

### Restore Command

`restore_command` example for postgresql.conf:

```ini
# where 'k8s-worker5:30266' represents the host and port
# of a 'pgrwl' instance running in 'serve' mode.
restore_command = 'pgrwl restore-command --serve-addr=k8s-worker5:30266 %f %p'
```

---

## Configuration Reference

**[`^        back to top        ^`](#table-of-contents)**

The configuration file is in JSON or YML format (\*.json is preferred).
It supports environment variable placeholders like `${PGRWL_SECRET_ACCESS_KEY}`.

You may either use `pgrwl start -c config.yml -m receive` or provide the corresponding environment variables and run
`pgrwl start`.

```
main:                                    # Required for both modes: receive/serve
  listen_port: 7070                      # HTTP server port (used for management)
  directory: "/var/lib/pgwal"            # Base directory for storing WAL files

receiver:                                # Required for 'receive' mode
  slot: replication_slot                 # Replication slot to use
  no_loop: false                         # If true, do not loop on connection loss
  uploader:                              # Required for non-local storage type
    sync_interval: 10s                   # Interval for the upload worker to check for new files
    max_concurrency: 4                   # Maximum number of files to upload concurrently
  retention:                             # Optional
    enable: true                         # Perform retention rules
    sync_interval: 10s                   # Interval for the retention worker (shouldn't run frequently - 12h is typically sufficient)
    keep_period: "1m"                    # Remove WAL files older than given period

backup:                                  # Required for 'backup' mode
  cron: "0 0 */3 * *"                    # Basebackup cron schedule
  retention:                             # Optional
    enable: true                         # Perform retention rules
    type: time                           # One of: (time / count)
    value: "48h"                         # Remove backups older than given period (if time), keep last N backups (if count)
    keep_last: 1                         # Always keep last N backups (suitable when 'retention.type = time')
  walretention:                          # Optional (WAL archive cleanup settings)
    enable: true                         # After basebackup is done, cleanup WAL-archive by oldest backup stop-LSN
    receiver_addr: "pgrwl-receive:7070"  # Address or WAL-receiver instance (required when manage_cleanup is set to true)

log:                                     # Optional
  level: info                            # One of: (trace / debug / info / warn / error)
  format: text                           # One of: (text / json)
  add_source: true                       # Include file:line in log messages (for local development)

metrics:                                 # Optional
  enable: true                           # Optional (used in receive mode: http://host:port/metrics)

devconfig:                               # Optional (various dev options)
  pprof:                                 # pprof settings
    enable: true                         # Enable pprof handlers

storage:                                 # Optional
  name: s3                               # One of: (s3 / sftp)
  compression:                           # Optional
    algo: gzip                           # One of: (gzip / zstd)
  encryption:                            # Optional
    algo: aes-256-gcm                    # One of: (aes-256-gcm)
    pass: "${PGRWL_ENCRYPT_PASSWD}"      # Encryption password (from env)
  sftp:                                  # Required section for 'sftp' storage
    host: sftp.example.com               # SFTP server hostname
    port: 22                             # SFTP server port
    user: backupuser                     # SFTP username
    pass: "${PGRWL_VM_PASSWORD}"         # SFTP password (from env)
    pkey_path: "/home/user/.ssh/id_rsa"  # Path to SSH private key (optional)
    pkey_pass: "${PGRWL_SSH_PKEY_PASS}"  # Required if the private key is password-protected
    base_dir: "/mnt/wal-archive"         # Base directory with sufficient user permissions
  s3:                                    # Required section for 's3' storage
    url: https://s3.example.com          # S3-compatible endpoint URL
    access_key_id: AKIAEXAMPLE           # AWS access key ID
    secret_access_key: "${PGRWL_AWS_SK}" # AWS secret access key (from env)
    bucket: postgres-backups             # Target S3 bucket name
    region: us-east-1                    # S3 region
    use_path_style: true                 # Use path-style URLs for S3
    disable_ssl: false                   # Disable SSL
```

Corresponding env-vars.

```
PGRWL_MAIN_LISTEN_PORT                   # HTTP server port (used for management)
PGRWL_MAIN_DIRECTORY                     # Base directory for storing WAL files
PGRWL_RECEIVER_SLOT                      # Replication slot to use
PGRWL_RECEIVER_NO_LOOP                   # If true, do not loop on connection loss
PGRWL_RECEIVER_UPLOADER_SYNC_INTERVAL    # Interval for the upload worker to check for new files
PGRWL_RECEIVER_UPLOADER_MAX_CONCURRENCY  # Maximum number of files to upload concurrently
PGRWL_RECEIVER_RETENTION_ENABLE          # Perform retention rules
PGRWL_RECEIVER_RETENTION_SYNC_INTERVAL   # Interval for the retention worker (shouldn't run frequently - 12h is typically sufficient)
PGRWL_RECEIVER_RETENTION_KEEP_PERIOD     # Remove WAL files older than given period
PGRWL_BACKUP_CRON                        # Basebackup cron schedule
PGRWL_BACKUP_RETENTION_ENABLE            # Perform retention rules
PGRWL_BACKUP_RETENTION_TYPE              # One of: (time / count)
PGRWL_BACKUP_RETENTION_VALUE             # Remove backups older than given period (if time), keep last N backups (if count)
PGRWL_BACKUP_RETENTION_KEEP_LAST         # Always keep last N backups (suitable when 'retention.type = time')
PGRWL_BACKUP_WALRETENTION_ENABLE         # After basebackup is done, cleanup WAL-archive by oldest backup stop-LSN
PGRWL_BACKUP_WALRETENTION_RECEIVER_ADDR  # Address or WAL-receiver instance (required when manage_cleanup is set to true)
PGRWL_LOG_LEVEL                          # One of: (trace / debug / info / warn / error)
PGRWL_LOG_FORMAT                         # One of: (text / json)
PGRWL_LOG_ADD_SOURCE                     # Include file:line in log messages (for local development)
PGRWL_METRICS_ENABLE                     # Optional (used in receive mode: http://host:port/metrics)
PGRWL_DEVCONFIG_PPROF_ENABLE             # Enable pprof handlers
PGRWL_STORAGE_NAME                       # One of: (s3 / sftp)
PGRWL_STORAGE_COMPRESSION_ALGO           # One of: (gzip / zstd)
PGRWL_STORAGE_ENCRYPTION_ALGO            # One of: (aes-256-gcm)
PGRWL_STORAGE_ENCRYPTION_PASS            # Encryption password (from env)
PGRWL_STORAGE_SFTP_HOST                  # SFTP server hostname
PGRWL_STORAGE_SFTP_PORT                  # SFTP server port
PGRWL_STORAGE_SFTP_USER                  # SFTP username
PGRWL_STORAGE_SFTP_PASS                  # SFTP password (from env)
PGRWL_STORAGE_SFTP_PKEY_PATH             # Path to SSH private key (optional)
PGRWL_STORAGE_SFTP_PKEY_PASS             # Required if the private key is password-protected
PGRWL_STORAGE_SFTP_BASE_DIR              # Base directory with sufficient user permissions
PGRWL_STORAGE_S3_URL                     # S3-compatible endpoint URL
PGRWL_STORAGE_S3_ACCESS_KEY_ID           # AWS access key ID
PGRWL_STORAGE_S3_SECRET_ACCESS_KEY       # AWS secret access key (from env)
PGRWL_STORAGE_S3_BUCKET                  # Target S3 bucket name
PGRWL_STORAGE_S3_REGION                  # S3 region
PGRWL_STORAGE_S3_USE_PATH_STYLE          # Use path-style URLs for S3
PGRWL_STORAGE_S3_DISABLE_SSL             # Disable SSL
```

---

## Installation

**[`^        back to top        ^`](#table-of-contents)**

### Docker images are available at [quay.io/hashmap_kz/pgrwl](https://quay.io/repository/hashmap_kz/pgrwl)

```bash
docker pull quay.io/hashmap_kz/pgrwl:latest
```

### Manual Installation

1. Download the latest binary for your platform from
   the [Releases page](https://github.com/hashmap-kz/pgrwl/releases).
2. Place the binary in your system's `PATH` (e.g., `/usr/local/bin`).

### Installation script for Unix-Based OS _(requires: tar, curl, jq)_:

```bash
(
set -euo pipefail

OS="$(uname | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m | sed -e 's/x86_64/amd64/' -e 's/\(arm\)\(64\)\?.*/\1\2/' -e 's/aarch64$/arm64/')"
TAG="$(curl -s https://api.github.com/repos/hashmap-kz/pgrwl/releases/latest | jq -r .tag_name)"

curl -L "https://github.com/hashmap-kz/pgrwl/releases/download/${TAG}/pgrwl_${TAG}_${OS}_${ARCH}.tar.gz" |
tar -xzf - -C /usr/local/bin && \
chmod +x /usr/local/bin/pgrwl
)
```

### Package-Based installation (suitable in CI/CD)

#### Debian

```bash
sudo apt update -y && sudo apt install -y curl
curl -LO https://github.com/hashmap-kz/pgrwl/releases/latest/download/pgrwl_linux_amd64.deb
sudo dpkg -i pgrwl_linux_amd64.deb
```

#### Apline Linux

```bash
apk update && apk add --no-cache bash curl
curl -LO https://github.com/hashmap-kz/pgrwl/releases/latest/download/pgrwl_linux_amd64.apk
apk add pgrwl_linux_amd64.apk --allow-untrusted
```

### Helm Chart

See [pgrwl-helm-chart](charts/pgrwl)

```bash
helm repo add pgrwl https://hashmap-kz.github.io/pgrwl
helm repo update
```

To install the chart with the release name `pgrwl`:

```bash
helm install pgrwl pgrwl/pgrwl
```

---

## Disaster Recovery Use Cases

**[`^        back to top        ^`](#table-of-contents)**

_The full process may look like this (a typical, rough, and simplified example):_

- A typical production setup runs **two `pgrwl` StatefulSets** in the cluster:
  one in `receive` mode for **continuous WAL streaming**, and another in `backup` mode for scheduled **base backups**.

- In `receive` mode, `pgrwl` continuously **streams WAL files**, applies optional **compression** and **encryption**,
  uploads them to **remote storage** (such as S3 or SFTP), and enforces **retention policies** - for example, keeping
  WAL files for **four days**.

- In `backup` mode, it performs a **full base backup** of your PostgreSQL cluster on a configured schedule -
  for instance, **once every three days** - using **streaming basebackup**, with optional **compression**
  and **encryption**. The resulting backup is also uploaded to the configured **remote storage**,
  and subject to **retention policies** for cleanup. The built-in cron scheduler enables fully automated backups without
  requiring external orchestration.

- During recovery, the same `receive` StatefulSet can be reconfigured to run in `serve` mode,
  exposing previously archived WALs via HTTP to support **Point-in-Time Recovery (PITR)** through `restore_command`.

- With this setup, you're able to restore your cluster - in the event of a crash -
  to **any second within the past three days**, using the most recent base backup and available WAL segments.

---

## Architecture

**[`^        back to top        ^`](#table-of-contents)**

### Design Notes

`pgrwl` is designed to **always stream WAL data to the local filesystem first**. This design ensures durability and
correctness, especially in synchronous replication setups where PostgreSQL waits for the replica to confirm the commit.

- Incoming WAL data is written directly to `*.partial` files in a local directory.
- These `*.partial` files are synced (`fsync`) after each write to ensure that WAL segments are fully durable on disk.
- Once a WAL segment is fully received, the `*.partial` suffix is removed, and the file is considered complete.

**Compression and encryption** are applied only after a WAL segment is completed:

- Completed files are passed to the uploader worker, which may compress and/or encrypt them before uploading to a remote
  backend (e.g., S3, SFTP).
- The uploader worker **ignores partial files** and operates only on finalized, closed segments.

This model avoids the complexity and risk of streaming incomplete WAL data directly to remote storage, which can lead to
inconsistencies or partial restores. By ensuring that all WAL files are locally durable and only completed files are
uploaded, `pgrwl` guarantees restore safety and clean segment handoff for disaster recovery.

In short: **PostgreSQL requires acknowledgments for commits in synchronous setups**, and relying on external systems for
critical paths (like WAL streaming) could introduce unacceptable delays or failures. This architecture mitigates that
risk.

### Durability & `fsync`

- After each WAL segment is written, an `fsync` is performed on the currently open WAL file to ensure durability.
- An `fsync` is triggered when a WAL segment is completed and the `*.partial` file is renamed to its final form.
- An `fsync` is triggered when a keepalive message is received from the server with the `reply_requested` option set.
- Additionally, `fsync` is called whenever an error occurs during the receive-copy loop.

### Why Not `archive_command`?

There’s a significant difference between using `archive_command` and archiving WAL files via the streaming replication
protocol.

The `archive_command` is triggered only after a WAL file is fully completed-typically when it reaches 16 MiB (the
default segment size). This means that in a crash scenario, you could lose up to 16 MiB of data.

You can mitigate this by setting a lower `archive_timeout` (e.g., 1 minute), but even then, in a worst-case scenario,
you risk losing up to 1 minute of data.
Also, it’s important to note that PostgreSQL preallocates WAL files to the configured `wal_segment_size`, so they are
created with full size regardless of how much data has been written. (Quote from documentation:
_It is therefore unwise to set a very short `archive_timeout` - it will bloat your archive storage._).

In contrast, streaming WAL archiving-when used with replication slots and the `synchronous_standby_names`
parameter-ensures that the system can be restored to the latest committed transaction.
This approach provides true zero data loss (**RPO=0**), making it ideal for high-durability requirements.

## Contributing

**[`^        back to top        ^`](#table-of-contents)**

Contributions are welcomed and greatly appreciated. See [CONTRIBUTING.md](./CONTRIBUTING.md)
for details on submitting patches and the contribution workflow.

Check also the [Developer Notes](docs/developer_notes.md) for additional information and guidelines.

---

### Links

**[`^        back to top        ^`](#table-of-contents)**

- [pg_receivewal Documentation](https://www.postgresql.org/docs/current/app-pgrwl.html)
- [pg_receivewal Source Code](https://github.com/postgres/postgres/blob/master/src/bin/pg_basebackup/pg_receivewal.c)
- [Streaming Replication Protocol](https://www.postgresql.org/docs/current/protocol-replication.html)
- [Continuous Archiving and Point-in-Time Recovery](https://www.postgresql.org/docs/current/continuous-archiving.html)
- [Setting Up WAL Archiving](https://www.postgresql.org/docs/current/continuous-archiving.html#BACKUP-ARCHIVING-WAL)

---

## License

MIT License. See [LICENSE](./LICENSE) for details.
