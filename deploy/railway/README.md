# Railway Service Configuration

Railway treats each service as a separate deployment. Create these services from the same GitHub repository and assign each repository-absolute config path in **Settings > Config as Code**:

| Service | Config path | Public domain |
|---|---|---|
| `api` | `/deploy/railway/api.json` | Required |
| `web` | `/deploy/railway/web.json` | Required |
| `alerts-cron` | `/deploy/railway/alerts-cron.json` | No |
| `gtfs-cron` | `/deploy/railway/gtfs-cron.json` | No |

Keep the root directory `/` for all four services. The Dockerfile paths are repository-relative.

## Managed Resources

Create:

- A Railway PostgreSQL 17 service named `Postgres`.
- A private Railway Storage Bucket named `raw-archive` in the same region as the workers.

The bucket stores source protobuf and GTFS bytes. It is not exposed to browsers. Railway buckets currently do not provide lifecycle rules, object locking, versioning, or automatic bucket backups, so retention and independent export remain operational responsibilities.

## Public Domains

Generate the API and web domains before adding cross-service variables. Browsers cannot use Railway private DNS.

Set:

```text
api.CORS_ALLOWED_ORIGIN=https://${{web.RAILWAY_PUBLIC_DOMAIN}}
web.VITE_API_BASE_URL=https://${{api.RAILWAY_PUBLIC_DOMAIN}}
```

`VITE_API_BASE_URL` is a Docker build argument and is embedded into the browser bundle. Redeploy the web service after changing the API domain.

## API Variables

```text
DATABASE_URL=${{Postgres.DATABASE_URL}}
CORS_ALLOWED_ORIGIN=https://${{web.RAILWAY_PUBLIC_DOMAIN}}
API_REQUEST_TIMEOUT=15s
API_SHUTDOWN_TIMEOUT=10s
STATUS_ALERT_DATA_MAX_AGE=10m
STATUS_ALERT_CHECK_MAX_AGE=10m
STATUS_GTFS_DATA_MAX_AGE=192h
STATUS_GTFS_CHECK_MAX_AGE=36h
STATUS_ALERT_RUN_MAX_DURATION=5m
STATUS_GTFS_RUN_MAX_DURATION=30m
STATUS_FUTURE_TOLERANCE=2m
STATUS_RECENT_FAILURE_LIMIT=5
```

Railway injects `PORT`; do not hard-code it. The pre-deploy command runs embedded database migrations before a new API deployment becomes active.

## Shared Worker Variables

Add these to both cron services:

```text
DATABASE_URL=${{Postgres.DATABASE_URL}}
RAW_STORAGE_BACKEND=s3
RAW_STORAGE_S3_BUCKET=${{raw-archive.BUCKET}}
RAW_STORAGE_S3_REGION=${{raw-archive.REGION}}
RAW_STORAGE_S3_ENDPOINT=${{raw-archive.ENDPOINT}}
RAW_STORAGE_S3_ACCESS_KEY_ID=${{raw-archive.ACCESS_KEY_ID}}
RAW_STORAGE_S3_SECRET_ACCESS_KEY=${{raw-archive.SECRET_ACCESS_KEY}}
RAW_STORAGE_S3_PATH_STYLE=false
```

Do not add storage credentials to the API or web services.

## Alert Worker Variables

```text
TRANSIT_API_KEY=<secret>
TRANSIT_API_KEY_HEADER=KeyID
TRANSIT_ALERTS_URL=https://api.opendata.transport.vic.gov.au/opendata/public-transport/gtfs/realtime/v1/metro/service-alerts
TRANSIT_HTTP_TIMEOUT=15s
```

The schedule is every five minutes, Railway's minimum cron interval. Railway skips a trigger if the previous execution is still running. Failed cron containers are not restarted; the next scheduled run retries naturally.

## GTFS Worker Variables

```text
GTFS_URL=https://data.ptv.vic.gov.au/downloads/gtfs.zip
GTFS_HTTP_TIMEOUT=10m
```

The schedule is `17:15 UTC` daily. Content hashes prevent an unchanged dataset from replacing the current network, although every successful check is still archived before interpretation.

## Backups

In the Railway dashboard:

1. Enable daily and weekly PostgreSQL volume backups.
2. Enable PostgreSQL point-in-time recovery for production.
3. Perform a restore into a sibling PostgreSQL service before relying on the policy.
4. Record the restore and connection-cutover procedure outside the source database.

Railway restores do not automatically switch application `DATABASE_URL` references.

## Activation Order

1. Provision PostgreSQL and the storage bucket.
2. Generate API and web domains.
3. Add variables and deploy the API so migrations run.
4. Deploy the web service and verify deep links.
5. Manually run GTFS ingestion once.
6. Manually run alert ingestion once.
7. Verify `/api/v1/status` and archived objects.
8. Enable both cron schedules.
9. Run `scripts/smoke-production.sh`.

See the external Chunk 12 documentation for failure modes, restore checks, and operational monitoring.
