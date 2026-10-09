# Deployment

Operating notes: what constraints this backend imposes and how to back it up and restore it. Deployment itself is any way of running the binary or the container behind a proxy with TLS.

## Project constraints

- **A single instance, always.** SQLite with WAL supports many readers but a single writer, and the file lives on a local volume: do not scale the application nor share that volume between previews or replicas.
- **Local disk, nothing networked.** WAL uses `mmap` and POSIX locks, so `DATABASE_URL` cannot point at NFS or network storage.
- **Behind a proxy that is not loopback** (nginx/Caddy on another network, containers, any PaaS) that network must be declared in `TRUSTED_PROXY_CIDRS`, otherwise every client shares the same limit bucket (30 logins per minute across all of them) and the logs record the proxy IP instead of the client one. Example to get the subnet of a container network:

```sh
docker network inspect proxy --format '{{range .IPAM.Config}}{{.Subnet}}{{end}}'
# e.g. 10.0.0.0/16
```

See [Configuration](configuration.md) for how the `X-Forwarded-For` chain is walked.

## Variables in production

| Variable | Value |
|----------|-------|
| `ENV` | `prod` |
| `JWT_SECRET` | `openssl rand -base64 32`, marked as a secret and kept out of the repo |
| `DATABASE_URL` | SQLite file on the persistent volume, e.g. `/data/pinolrent.db` |
| `UPLOAD_DIR` | persistent image directory, e.g. `/data/uploads` |
| `CORS_ALLOWED_ORIGINS` | frontend origin(s), comma-separated (`*` is rejected with `ENV=prod`) |
| `TRUSTED_PROXY_CIDRS` | reverse proxy network (see above) |
| `PORT` | `8080` |

The container must mount `/data` as a persistent volume: that is where the database lives (`pinolrent.db` with its `-wal`/`-shm`) along with `uploads/`. The healthcheck hits `GET /health` (port 8080). The image reports the version from the `VERSION` build-arg (CI passes `ci-<sha>`); without it the build falls back to `git describe`, which finds no `.git` in the build context (it is in `.dockerignore`) and answers `dev`.

## Deploy and rollback

1. Push to `main`: the deployment platform, with auto-deploy (webhook) connected to the repo, rebuilds the image and replaces the container. Migrations apply themselves on startup and are additive.
2. Verify: `curl -fsS https://api.<domain>/health` → `{"status":"ok","version":"v0.1.0-N-g<sha>"}` — the version must match the commit you just pushed; if it says `dev`, the build had no access to `.git`.
3. If anything goes wrong, redeploy the previous commit from the platform and repeat the verification. As long as the migrations stay additive, an old binary works against the new schema.

Releases are tagged `vX.Y.Z` (`git tag -a vX.Y.Z -m "..."`). Between tags the reported version combines the last tag with the sha: `v0.1.0-3-g2627c64`.

## Backups

A file-level copy of a database that is being written can come out inconsistent, so there are two layers:

1. **Consistent snapshot** (daily job): `VACUUM INTO` uses the SQLite engine over the live database — the result is a complete, compact copy. Run `scripts/backup.sh` from cron on the host that holds `/data` (see the header of the script for the cron line); it snapshots and prunes copies older than 14 days:

   ```sh
   DATABASE_URL=/data/pinolrent.db ./scripts/backup.sh
   ```

2. **Off-site copy**: the snapshots and `uploads/` live on the server's own disk, so a broken disk takes them with it. Upload them to any S3-compatible storage (Object Storage, R2, B2…) with independent retention: the local copy to restore fast, the remote one to survive the loss of the server.

> A backup without a tested restore is not a backup: run the exercise in the next section at least once.

## Restoration

1. Download the copy (the backup file or the daily volume snapshot).
2. Keep the `.db` from `/data/backups/`, which is the consistent copy.
3. Check that it works:

   ```sh
   sqlite3 pinolrent-<date>.db "PRAGMA integrity_check;"
   sqlite3 pinolrent-<date>.db "SELECT count(*) FROM users;"
   ```

4. With the process stopped, replace the database with that copy and delete the old `-wal` / `-shm` (they belong to the previous database).
5. Start again and verify `/health` and that the data is there.

## Known limitations

- With a daily snapshot you can lose up to 24 h of data if the server dies; the off-site copy does not change that, it only avoids losing everything.
- `uploads/` has a total quota (`UPLOAD_MAX_TOTAL_MB`, default 1 GB) and a sweep every 6 hours deletes orphan files older than 7 days; even so it is worth checking the disk every so often.
- `WriteTimeout` is 120 s: the whole request body is capped at 5.5 MB (5 MB of
  image plus multipart overhead), so an upload needs a sustained ~48 KB/s to
  land. Below that it can still get cut.
- The rate limit is in memory and per process: with a single instance that is correct, but restarting the process resets it.