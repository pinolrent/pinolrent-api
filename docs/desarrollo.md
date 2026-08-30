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
| `make test` | `go test -count=1 -timeout 120s ./...` |
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
al arrancar** el server (azure goose embebido) — no hay paso manual en dev.
Para crear una migración nueva:

```sh
go run github.com/pressly/goose/v3/cmd/goose@latest create add_<algo> sql
```

Mueve el archivo generado (timestamp) a `internal/db/migrations/` y edita las
secciones `-- +goose Up` / `-- +goose Down`. goose registra el historial en la
tabla `goose_db_version` y **no borra datos**. Al migrar desde la versión
pre-goose, la `dev.db` conserva sus tablas y filas; no hace falta borrarla.

Si no tenés GNU make en tu shell de WSL (o Windows), podés correr los mismos
comandos directo con `go`: `go test ./...`, `go build ./...`,
`go run ./cmd/api`, etc.

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

Cobertura actual por paquete: config (validate), db (schema + seed + overlap),
handlers (auth, cars, reservations, payments, hardening, helpers) y ratelimit
(token bucket + middleware). El flujo E2E se puede ver en
[flujo-completo](flujo-completo.md) y el smoke automatizado en
`scripts/demo.sh`.