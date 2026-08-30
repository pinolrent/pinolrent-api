# Desarrollo

## Quickstart

```sh
make tools        # una vez: instala air (hot-reload) y golangci-lint v2
make dev          # arranca el server con defaults de desarrollo
```

## Makefile

| Target | Qué hace |
|--------|----------|
| `make help` | Lista los targets disponibles |
| `make tools` | Instala `air` (`go install`) y `golangci-lint` v2 (binario oficial via su `install.sh`) |
| `make dev` | Arranca el server vía `scripts/dev.sh` (defaults de dev, carga `.env`) |
| `make watch` | Hot-reload con `air`: rebuild automático al editar `.go`, `.env` o `.toml` |
| `make run` | `go run ./cmd/api` directo (fail-fast real de variables) |
| `make build` | Compila el binario a `bin/pinolrent-api` inyectando la versión (`git describe`, o `dev` si no hay repo) |
| `make test` | `go test -count=1 -timeout 120s ./...` (sin CGO, sin `-race`) |
| `make test-race` | `CGO_ENABLED=1 go test -count=1 -race -timeout 180s ./...` (requiere gcc) |
| `make cover` | Corre la suite con `-coverprofile` y muestra un resumen de cobertura |
| `make vet` | `go vet ./...` |
| `make lint` | `golangci-lint run ./...` (debe quedar en **0 issues**) |
| `make fmt` | `gofmt -w` de todo el árbol |
| `make tidy` | `go mod tidy` |
| `make demo` | Smoke E2E autocontenido en `:8132` con DB efímera |
| `make clean` | Borra binarios y artefactos de build |

Herramientas instaladas: `air` y `golangci-lint` viven en `$(go env GOPATH)/bin`
(por defecto `~/go/bin`); asegurate de que esté en tu `PATH`.

## Configuracion del linter

- `.golangci.yml` en formato **v2**: linters de la serie `standard` más
  `revive`, `gosec`, `noctx` y `misspell`.
- Reglas de estilo del repo:
  - Comentarios/docstrings en **inglés**; docs y README en **español**.
  - Todo símbolo exportado lleva doc comment (`revive` lo exige).
  - `gofmt` siempre limpio; los archivos terminan con newline final.

## Migraciones

El esquema vive en `internal/db/migrations/*.sql` y se aplica **automáticamente
al arrancar** el server vía goose embebido — no hay paso manual en dev.
Para crear una migración nueva:

```sh
go run github.com/pressly/goose/v3/cmd/goose@latest create add_<algo> sql
```

Mueve el archivo generado (timestamp) a `internal/db/migrations/` y edita las
secciones `-- +goose Up` / `-- +goose Down`. goose registra el historial en la
tabla `goose_db_version` y **no borra datos**: una base existente conserva sus
tablas y filas al abrir la API; no hace falta borrarla al actualizar.

## Collection de Bruno

`bruno/pinolrent-api/` es una colección versionada y lista para correr:

- `bruno/pinolrent-api/collection.bru` — define variables runtime
  (`baseUrl`, `sellerEmail`, `sellerPassword`, `buyerEmail`, `buyerPassword`).
- Los requests de login capturan el token automáticamente y lo guardan en
  `sellerToken`/`buyerToken` con un `script:post-response`.
- `carId` y `reservationId` se inyectan igual al crear el auto/la reserva.

Corre la colección **en orden** (los requests dependen de los anteriores):
Registrar comprador → Registrar vendedor → Login vendedor → Login comprador →
Mi perfil → Listar autos → Crear auto → Detalle auto → Activar auto → Listar
mis autos → Crear reserva → Mis reservas → Detalle reserva → Registrar pago →
Cancelar reserva (devuelve `409 payment already recorded, cannot cancel`, raro
pero esperado en este punto) → Reservas de mis autos → Confirmar reserva.
Ajusta
`baseUrl` si no es `http://localhost:8080`.

## Tests

```sh
make test
make lint
```

Cobertura actual por paquete: `auth` (bcrypt, JWT, middleware `RequireAuth` y
`RequireRole`, parser endurecido con `iss`/`aud`/`exp`) al 100%, `config`
(validate, CORS), `db` (schema, migraciones, overlap), `handlers` (auth, cars,
reservations, payments, hardening, helpers) y `ratelimit` (token bucket,
middleware, prefix matching). El flujo E2E se puede ver en
[flujo-completo](flujo-completo.md) y el smoke automatizado en
`scripts/demo.sh`.

## CI

`.github/workflows/ci.yml` corre en cada `push` a `main` y en cada pull
request: `make vet`, `make test`, `make test-race`, `golangci-lint v2.13.2`,
`make build` y `make demo`. La versión de `golangci-lint` está pineada en el
workflow y debe coincidir con `GOLANGCI_VERSION` del Makefile.