# Desarrollo

## Arranque rápido

```sh
make tools        # solo la primera vez: instala air, govulncheck y golangci-lint en ~/go/bin (agrégalo a tu PATH)
make dev          # levanta el server con valores de desarrollo (con defaults, sin fallar)
make run          # levanta sin defaults (falla sin JWT_SECRET, como en prod)
```

## Makefile

| Comando | Qué hace |
|---------|----------|
| `make help` | Lista los comandos |
| `make tools` | Instala `air`, `govulncheck` y `golangci-lint v2` |
| `make dev` | Levanta el server con `scripts/dev.sh` (valores de dev + `.env` si existe) |
| `make run` | `go run ./cmd/api` directo (pide variables reales, sin defaults de dev) |
| `make watch` | Recarga automática al editar `.go` o `.toml` (requiere `make tools`) |
| `make build` | Compila a `bin/pinolrent-api` con la versión del git |
| `make test` | Corre los tests (`go test -count=1 -timeout 180s ./...`) |
| `make test-race` | Tests con detector de carreras (`CGO_ENABLED=1`, necesita gcc, 300s) |
| `make cover` | Corre tests y muestra resumen de cobertura (180s, mínimo 70% salvo `COVER_MIN=N`) |
| `make vuln` | `govulncheck ./...` |
| `make vet` | `go vet ./...` |
| `make lint` | `golangci-lint run ./...` (debe quedar en 0) |
| `make fmt` | Formatea con `gofmt -w` |
| `make tidy` | `go mod tidy` |
| `make demo` | Prueba completa automática en `:8132` con base temporal |
| `make clean` | Borra `bin/`, `tmp/`, `air.log` y base de dev |

`air`, `govulncheck` y `golangci-lint` quedan en `$(go env GOPATH)/bin` (normalmente `~/go/bin`); asegúrate de tenerlo en tu `PATH`. `test-race` necesita `gcc`.

## Linter

- Config en `.golangci.yml` formato **v2**: linters estándar + `revive`, `gosec`, `noctx` y `misspell`.
- Reglas del repo:
  - Todo lo exportado lleva comentario.
  - `gofmt` limpio y archivos terminan con salto de línea.

## Migraciones

El esquema está en `internal/db/migrations/*.sql` y se aplica **solo al arrancar** — no hay paso manual en dev.

Para crear una nueva:

```sh
go run github.com/pressly/goose/v3/cmd/goose@latest create add_<algo> sql
```

Mueve el archivo generado a `internal/db/migrations/` y edita `Up`/`Down`. Queda registrado en `goose_db_version` y no borra datos de bases existentes.

## Colección Bruno

`bruno/pinolrent-api/` está lista para correr:

- `collection.bru` define variables (`baseUrl`, `sellerEmail`, `buyerEmail`, etc.).
- Los logins guardan el token y el refresh automáticamente en `sellerToken`/`buyerToken` (más `sellerRefresh`/`buyerRefresh`).
- `carId` y `reservationId` se guardan igual al crear auto/reserva.
- Cada request trae `assert` de status y campos clave, así el CLI falla si algo cambia.

Corre los requests **en orden** (`seq`, dependen unos de otros). Cambia `baseUrl` si no es `http://localhost:8080`. También corre en CI (job `bruno` con `@usebruno/cli`).

## Tests

```sh
make test
make lint
```

Los tests cubren `auth`, `config`, `db`, `handlers`, `httpx`, `models` y `ratelimit` (viven junto al código como `*_test.go`). El flujo completo se puede ver en [flujo-completo](flujo-completo.md) y la prueba automática en `scripts/demo.sh`.

## CI

`.github/workflows/ci.yml` corre en cada push a `main` y en cada PR, en jobs paralelos: `test` (`go mod verify`, `gofmt`, `tidy`, `make vet`, `make test`, `make test-race`, `make cover`), `lint` (`golangci-lint v2.13.2`), `vuln` (`govulncheck`), `build-demo` (`make build` y `make demo`, 36 checks) y `bruno` (colección Bruno contra un server efímero). `concurrency` cancela corridas viejas del mismo branch. Las versiones de las herramientas en el workflow deben coincidir con `GOLANGCI_VERSION` / `GOVULNCHECK_VERSION` del Makefile.
