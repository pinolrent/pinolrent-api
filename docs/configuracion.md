# Configuración

## Variables de entorno

| Variable | Valor por defecto | ¿Obligatoria? | Para qué sirve |
|----------|-------------------|---------------|----------------|
| `PORT` | `8080` | no | Puerto donde escucha el server |
| `DATABASE_URL` | `pinolrent.db` | no | Dónde está el archivo SQLite |
| `JWT_SECRET` | — | **sí** | Secreto para firmar los tokens (mínimo 32 caracteres) |
| `CORS_ALLOWED_ORIGINS` | `*` | no | Qué orígenes pueden llamar a la API, separados por coma. `*` = todos |
| `ENV` | `dev` | no | Entorno: `dev` (default) o `prod`/`production`. En prod `CORS_ALLOWED_ORIGINS=*` es rechazado |

El orden de prioridad es: **variables del shell > `.env` > valores por defecto**. Si una variable está vacía se ignora.

### Si falta el secreto, no arranca

```sh
$ go run ./cmd/api
# ERROR: missing required env vars: JWT_SECRET
```

```sh
$ JWT_SECRET=short go run ./cmd/api
# ERROR: JWT_SECRET must be at least 32 bytes (got 5)
```

Esto lo valida `Config.Validate` en `internal/config/config.go`. Un secreto corto con HS256 es fácil de romper si alguien consigue un token, por eso se exige mínimo 32.

### Cómo generar un secreto

```sh
export JWT_SECRET="$(openssl rand -base64 32)"
```

## Arranque en producción

```sh
export JWT_SECRET="$(openssl rand -base64 32)"
go run ./cmd/api   # o el binario compilado con make build
```

### `ENV` y `PORT`

- `ENV` por defecto `dev`. Con `ENV=prod` o `ENV=production`, `CORS_ALLOWED_ORIGINS=*` es rechazado.
- `PORT` debe ser `1..65535` o el server no arranca.
- Un `.env` malformado (ej. línea sin `=`) hace que no arranque — se muestra el error en el log.

Qué pasa al arrancar:

1. Lee `.env` si existe (lo que ya está en el shell manda). Si está mal formado, se apaga.
2. Valida `JWT_SECRET` — si falta o es corto, se apaga. Valida `PORT` y `CORS_ALLOWED_ORIGINS`/`ENV`.
3. Abre SQLite con WAL y aplica las migraciones que falten (quedan registradas en `goose_db_version`, nunca borran datos). Si es `:memory:` usa una sola conexión, si no hasta 8 (con `MaxIdleTime` 5 min / `MaxLifetime` 30 min). Si la base está ocupada, reintenta con backoff.
4. Levanta el HTTP y espera `SIGINT`/`SIGTERM` para apagarse limpio (hasta 10 s).

No hay usuario admin: compradores y vendedores se crean con `POST /auth/register` y `POST /auth/register/seller`.

## Desarrollo

Con `make dev` no necesitas crear `.env`: `scripts/dev.sh` pone valores por defecto (`JWT_SECRET=dev-secret-not-for-production-32b`, `DATABASE_URL=dev.db`) y arranca con `go run`. Si corres `go run ./cmd/api` directo sin variables, sí te va a pedir el secreto (es el comportamiento real de producción).

Puedes copiar `.env.example` a `.env` para cambiar esos valores.

> `dev.db` y sus archivos (`-wal`, `-shm`) están en `.gitignore`: no los commitees.
