# Configuración

## Variables de entorno

| Variable | Default | Requerida | Descripción |
|----------|---------|-----------|-------------|
| `PORT` | `8080` | no | Puerto del servidor |
| `DATABASE_URL` | `pinolrent.db` | no | Ruta/DSN del archivo SQLite |
| `JWT_SECRET` | — | **sí** | Secreto HS256 para firmar tokens; el proceso **aborta si falta** |
| `CORS_ALLOWED_ORIGINS` | `*` | no | Orígenes permitidos, separados por coma; `*` acepta cualquiera. Ver [00-general](api/00-general.md) |

Precedencia de valores: **variables del shell > `.env` > defaults**. Las
variables que están vacías en el entorno se tratan como ausentes.

### Fail-fast

El server no arranca sin `JWT_SECRET`:

```sh
$ go run ./cmd/api
time=... level=ERROR msg="invalid config" error="missing required env vars: JWT_SECRET"
exit status 1
```

El validador es `Config.Validate` en `internal/config/config.go`.

### Proveerse un secreto aleatorio

```sh
export JWT_SECRET="$(openssl rand -base64 32)"
```

## Arranque en producción

```sh
export JWT_SECRET="$(openssl rand -base64 32)"
go run ./cmd/api   # o compila y ejecuta el binario
```

Secuencia de arranque:

1. `godotenv.Load()` — carga `.env` si existe (los valores del shell ganan).
2. `config.Load()` + `config.Validate()` — aborta si falta `JWT_SECRET`.
3. `db.Open(DATABASE_URL)` — abre SQLite con WAL, busy timeout de 5 s y un pool
   pequeño (8 conexiones) y aplica las **migraciones goose** embebidas si hay
   pendientes (historial en `goose_db_version`; las migraciones **nunca borran
   datos**). La URL `:memory:` usa una sola conexión.
4. Se inicia el servidor HTTP y se espera `SIGINT`/`SIGTERM` para un shutdown
   graceful (10 s de tope).

No hay cuenta admin global: los vendedores y compradores se crean por registro
público (`POST /auth/register` y `POST /auth/register/seller`).

## Entorno de desarrollo

Con `make dev` no hace falta crear `.env`: el script `scripts/dev.sh` aplica
defaults de desarrollo cuando falta la variable correspondiente
(`JWT_SECRET=dev-secret-not-for-production`, `DATABASE_URL=dev.db`) y arranca
con `go run`. El fail-fast de producción se conserva: invocar
`go run ./cmd/api` directamente sin variables sigue abortando.

Copia `.env.example` a `.env` para override de esos defaults.

> `dev.db` (y sus artefactos WAL) están en `.gitignore`: nunca commitees bases
> de desarrollo.