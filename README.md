# keep-it-mobile

A Go CLI tool that pulls a dashboard of bond and recession risk indicators from the FRED API.

## Setup

1. Get a free FRED API key at https://fred.stlouisfed.org/docs/api/api_key.html
2. Export the API key:
   ```bash
   export FRED_API_KEY=your_fred_api_key_here
   ```

## Usage

Run the tool:
```bash
go run ./cmd/keep-it-mobile
```

### Flags

- `-fred_api_key`: Override the `FRED_API_KEY` environment variable.
- `-timeout`: Set the HTTP timeout (default `10s`).
- `-json`: Output results in JSON format instead of a table.
- `-series`: Comma-separated list of FRED series to fetch (overrides default set).

## Testing

Run the tests:
```bash
go test ./...
```

