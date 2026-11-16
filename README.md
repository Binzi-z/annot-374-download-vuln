# download-service

Internal portal's asset downloader. Two endpoints:

| endpoint | purpose |
|---|---|
| `GET /download?url=<remote>` | fetch a remote asset and stream it back |
| `GET /file?name=<relative>` | stream a file from the asset directory |
| `GET /healthz` | liveness |

## Run

```bash
go build ./...
go run . -addr 127.0.0.1:8094 -assets assets
```

## Tests

```bash
go test ./... -count=1
```

`app_test.go` holds the endpoint suite. Most cases are green; the security cases
listed below are currently red — that is the state the audit report describes.

## Known audit findings

1. `GET /download` will fetch any URL it is given, including addresses on the
   internal network and non-HTTP schemes.
2. `GET /file` joins the requested name onto the asset directory without
   containing it, so `../` (raw or percent-encoded) escapes the directory.

Both are reproducible against a local instance; the audit team has not attached
a script. `testdata/` is empty on purpose — reproducers belong there.

## Layout

```
main.go        flags + server bootstrap
app.go         handlers (/download, /file, /healthz)
app_test.go    endpoint suite
assets/        served by /file (created on first run)
```
