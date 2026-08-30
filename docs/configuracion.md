# Configuración

## Variables de entorno

| Variable | Default | Requerida | Descripción |
|----------|---------|-----------|-------------|
| `PORT` | `8080` | no | Puerto del servidor |
| `DATABASE_URL` | `pinolrent.db` | no | Ruta/DSN del archivo SQLite |
| `JWT_SECRET` | — | **sí** | Secreto HS256 para firmar tokens; el proceso **aborta si falta** |
| `ADMIN_EMAIL` | `admin@pinolrent.com` | no | Email del admin que se siembra al arrancar |
| `ADMIN_PASSWORD` | — | **sí** | Password del admin; el proceso **aborta si falta** |

Precedencia de valores: **variables del shell > `.env` > defaults**. Las
variables que están vacías en el entorno se tratan como ausentes.

### Fail-fast

El server no arranca sin `JWT_SECRET` y `ADMIN_PASSWORD`:

```sh
$ go run ./cmd/api
time=... level=ERROR msg="invalid config" error="missing required env vars: JWT_SECRET, ADMIN_PASSWORD"
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
export ADMIN_PASSWORD="una-password-fuerte"
go run ./cmd/api   # o compila y ejecuta el binario
```

Secuencia de arranque:

1. `godotenv.Load()` — carga `.env` si existe (los valores del shell ganan).
2. `config.Load()` + `config.Validate()` — aborta si faltan vars requeridas.
3. `db.Open(DATABASE_URL)` — crea el esquema si no existe; SQLite con WAL,
   busy timeout de 5 s y un pool pequeño (8 conexiones). La URL `:memory:` usa
   una sola conexión.
4. `db.SeedAdmin(ADMIN_EMAIL, ADMIN_PASSWORD)`.
5. Se inicia el servidor HTTP y se espera `SIGINT`/`SIGTERM` para un shutdown
   graceful (10 s de tope).

### Seed del admin

- Si el email no existe, se crea la cuenta `admin`.
- Si ya existe como `admin`, **se actualiza su password** con `ADMIN_PASSWORD`
  (útil para rotar credenciales en cada deploy).
- Si el email ya pertenece a una cuenta `client`, el arranque **falla**.

```text
time=... level=ERROR msg="seed admin" error="ADMIN_EMAIL \"x@y.com\" conflicts with an existing client account"
exit status 1
```

## Entorno de desarrollo

Con `make dev` no hace falta crear `.env`: el script `scripts/dev.sh` aplica
defaults de desarrollo cuando falta la variable correspondiente
(`JWT_SECRET=dev-secret-not-for-production`, `ADMIN_PASSWORD=admin123`,
`DATABASE_URL=dev.db`) y arranca con `go run`. El fail-fast de producción se
conserva: invocar `go run ./cmd/api` directamente sin variables sigue
abortando.

Copia `.env.example` a `.env` para override de esos defaults.

> `dev.db` (y sus artefactos WAL) están en `.gitignore`: nunca commitees bases
> de desarrollo.