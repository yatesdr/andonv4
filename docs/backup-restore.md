# Backup and Restore System

## Overview

The backup and restore system provides automated and manual backup capabilities for Andon Canvas, supporting both S3-compatible object storage and central HTTP servers. Backups include the full configuration and database state.

## Backup System

The BackupManager runs a background loop that handles:

1. **Config change signals** — when config.json changes, pushes it to enabled targets
2. **Manual triggers** — via POST /api/backup/trigger
3. **Periodic timer** — configurable interval in minutes (default: disabled / 24h)

### S3-Compatible Backup

Uses any S3-compatible object store (AWS S3, MinIO, etc.).

**Station Prefix Format**

```
{slugified-station-name}-{last-8-chars-of-station-id}
```

**Config Push**

- Saves `{prefix}/config/latest.json`
- Archives to `{prefix}/config/history/{timestamp}.json`

**DB Push**

- Creates consistent snapshot via `VACUUM INTO`
- Compresses with gzip
- Saves as `{prefix}/db/snap-{slot}.db.gz`

**Ring Buffer**

- 3 slots (0, 1, 2) rotating
- Tracked by `{prefix}/db/meta.json`
- Prevents unbounded storage growth

**Manifest**

Bucket-root `_manifest.json` tracks all stations that have backed up to the bucket.

### Central Server HTTP Push

**Config Push**

```
POST {centralURL}/{stationID}/config
Content-Type: application/json
```

**DB Push**

```
POST {centralURL}/{stationID}/db
Content-Type: application/gzip
X-Slot: {slot}
```

### Backup Status

Tracks the following state:

- LastConfigPush
- LastFullBackup
- LastError
- LastErrorTime
- NextScheduled
- CurrentSlot

Each S3/HTTP push operation has a 2-minute timeout.

### Settings

Configured via the Settings page:

| Field | Description |
|-------|-------------|
| S3 Enabled | Enable S3 backup |
| S3 Endpoint | S3/MinIO endpoint (e.g. `s3.amazonaws.com`) |
| S3 Bucket | Bucket name |
| S3 Access Key | Access key ID |
| S3 Secret Key | Secret access key |
| S3 Use SSL | Use HTTPS for S3 |
| S3 Region | AWS region |
| Central Enabled | Enable central server push |
| Central URL | Base URL of central backup server |
| Periodic Minutes | Interval for automatic full backups (0 = disabled) |

## Restore

Restore is a CLI subcommand that runs BEFORE the server starts. It downloads config.json and the newest database snapshot, replacing local files.

### Usage

```
andon restore --source <s3|central> --station <id> [options]
```

### S3 Restore

```bash
andon restore --source s3 \
  --station <station-id-or-prefix> \
  --s3-endpoint <endpoint> \
  --s3-bucket <bucket> \
  --s3-access-key <key> \
  --s3-secret-key <secret> \
  [--s3-ssl] [--s3-region <region>]
```

**Station Resolution**

Station can be:
- Full UUID
- Station prefix
- Partial UUID suffix

The restore command reads `_manifest.json` to resolve the station, then downloads `config/latest.json` and the newest snapshot from `db/meta.json`.

### Central Restore

```bash
andon restore --source central \
  --station <station-id> \
  --central-url <url>
```

Downloads:
- Config from `{centralURL}/{stationID}/config`
- Database from `{centralURL}/{stationID}/db`

### What Gets Restored

- `config.json` — full configuration (screens, settings, mappings, targets)
- `andon.db` — SQLite database (state events, hourly counts, shift summaries)
- Stale WAL/SHM files are cleaned up automatically

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/backup/status` | Yes | Current backup status (last push times, errors, next scheduled) |
| `POST` | `/api/backup/trigger` | Yes | Trigger immediate full backup |

## Use Cases

**Disaster Recovery**

If a station fails or needs to be replaced, use the restore command to pull the latest config and database from backup storage.

**Station Cloning**

Restore from one station's backup to initialize a new station with the same configuration.

**Central Management**

Configure multiple stations to push to a central backup server for unified management and monitoring.

**Offline Backup**

Use S3-compatible storage (like MinIO) on a local network for air-gapped environments.
