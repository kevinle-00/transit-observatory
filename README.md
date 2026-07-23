# Transit Observatory

Transit Observatory is an MVP historical disruption observatory for Melbourne's metropolitan train network. The current implementation is the feed-validation worker described in the project plan.

## Feed Validation

The worker fetches Transport Victoria's Metro Train service-alert feed, decodes the GTFS-Realtime protobuf, and writes a structured JSON summary without persisting data.

Prerequisites:

- Go 1.26 or later
- A Transport Victoria Open Data API key

From the `transit-observatory` repository, configure and run it:

```sh
export TRANSIT_API_KEY='your-key'
go run ./cmd/worker ingest-alerts --dry-run
```

Optional configuration is documented in `.env.example`. The worker defaults to the live-validated `KeyID` authentication header even though the downloadable OpenAPI document currently specifies `Ocp-Apim-Subscription-Key`.

Run checks with:

```sh
go test ./...
go vet ./...
```

The dry-run report includes feed timestamps, alert content, active periods, informed route and stop IDs, and recursively counted unknown protobuf bytes. Non-zero unknown-byte counts indicate fields that are not part of the official GTFS-Realtime bindings and need further inspection before warehouse modelling.
