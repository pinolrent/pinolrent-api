# Desarrollo

## Arranque rápido

```sh
make tools        # solo la primera vez: instala air y golangci-lint
make dev          # levanta el server con valores de desarrollo
```

## Makefile

| Comando | Qué hace |
|---------|----------|
| `make help` | Lista los comandos |
| `make tools` | Instala `air` y `golangci-lint v2` |
| `make dev` | Levanta el server con `scripts/dev.sh` (valores de dev + `.env` si existe) |
| `make watch` | Recarga automática al editar `.go`, `.env` o `.toml` |
| `make run` | `go run ./cmd/api` directo (pide variables reales) |
| `make build` | Compila a `bin/pinolrent-api` con la versión del git |
| `make test` | Corre los tests (`go test -count=1 -timeout 120s ./...`) |
| `make test-race` | Tests con detector de carreras (`CGO_ENABLED=1`, necesita gcc) |
| `make cover` | Corre tests y muestra resumen de cobertura |
| `make vet` | `go vet ./...` |
| `make lint` | `golangci-lint run ./...` (debe quedar en 0) |
| `make fmt` | Formatea con `gofmt -w` |
| `make tidy` | `go mod tidy` |
| `make demo` | Prueba completa automática en `:8132` con base temporal |
| `make clean` | Borra binarios y base de dev |

`air` y `golangci-lint` quedan en `$(go env GOPATH)/bin` (normalmente `~/go/bin`); asegúrate de tenerlo en tu `PATH`.

## Linter

- Config en `.golangci.yml` formato **v2**: linters estándar + `revive`, `gosec`, `noctx` y `misspell`.
- Reglas del repo:
  - Comentarios del código en **inglés**, docs y README en **español**.
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
- Los logins guardan el token automáticamente en `sellerToken`/`buyerToken`.
- `carId` y `reservationId` se guardan igual al crear auto/reserva.

Corre los requests **en orden** (dependen unos de otros). Cambia `baseUrl` si no es `http://localhost:8080`.

## Tests

```sh
make test
make lint
```

Los tests cubren `auth`, `config`, `db`, `handlers` y `ratelimit`. El flujo completo se puede ver en [flujo-completo](flujo-completo.md) y la prueba automática en `scripts/demo.sh`.

## CI

`.github/workflows/ci.yml` corre en cada push a `main` y en cada PR: `make vet`, `make test`, `make test-race`, `golangci-lint v2.13.2`, `make build` y `make demo`. La versión del linter en el workflow debe coincidir con `GOLANGCI_VERSION` del Makefile.
