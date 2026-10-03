# Configuración

## Variables de entorno

| Variable | Valor por defecto | ¿Obligatoria? | Para qué sirve |
|----------|-------------------|---------------|----------------|
| `PORT` | `8080` | no | Puerto donde escucha el server |
| `DATABASE_URL` | `pinolrent.db` | no | Dónde está el archivo SQLite |
| `JWT_SECRET` | — | **sí** | Secreto para firmar los tokens (mínimo 32 caracteres, con entropía: al menos 16 bytes distintos) |
| `CORS_ALLOWED_ORIGINS` | `*` | no | Qué orígenes pueden llamar a la API, separados por coma. `*` = todos |
| `ENV` | `dev` | no | Entorno: `dev` (default) o `prod`/`production`. En prod `CORS_ALLOWED_ORIGINS=*` es rechazado |
| `UPLOAD_DIR` | `uploads` | no | Directorio donde se guardan las imágenes de `POST /uploads`. Se crea al arrancar; no vacío |
| `UPLOAD_MAX_TOTAL_MB` | `1024` | no | Cap total de `UPLOAD_DIR` en MB; `0` = sin límite. Al superarse, `POST /uploads` responde `507` |
| `TRUSTED_PROXY_CIDRS` | — (vacío) | no | Redes de las que se cree `X-Forwarded-For`/`X-Real-IP`, separadas por coma (ej. `172.18.0.0/16`). Vacío = solo se confía en un proxy en loopback |
| `ADMIN_EMAILS` | — (vacío) | no | Allow-list de cuentas con rol `admin`, separada por coma (ej. `admin@pinolrent.cl`). Vacío = la instalación no tiene administrador. Una entrada malformada impide arrancar |

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

```sh
$ JWT_SECRET=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa go run ./cmd/api
# ERROR: JWT_SECRET has too little entropy (only 1 unique bytes, want >= 16)
```

Esto lo valida `Config.Validate` en `internal/config/config.go`. Un secreto corto o repetitivo con HS256 es fácil de romper si alguien consigue un token, por eso se exige mínimo 32 caracteres con al menos 16 bytes distintos.

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

### Detrás de un proxy inverso

El rate limit por IP y los logs necesitan la IP real del cliente, así que la app solo lee `X-Forwarded-For` / `X-Real-IP` cuando el pedido viene de un proxy en el que confía:

- **Proxy en loopback** (nginx o Caddy en la misma máquina): confiable siempre, no hay que configurar nada.
- **Proxy en otra red** (contenedores, cualquier PaaS): hay que declarar esa red en `TRUSTED_PROXY_CIDRS`. Si no, todos los clientes comparten un mismo contador de límite (30 logins por minuto entre todos) y los logs registran la IP del proxy.

```sh
export TRUSTED_PROXY_CIDRS=172.18.0.0/16
```

Cuando el peer es confiable, la cadena de `X-Forwarded-For` se recorre **de derecha a izquierda** y se toma el primer valor que no sea a su vez un proxy confiable. Así, si un cliente manda un header inventado, no puede elegirse el propio bucket: la dirección que agregó nuestro proxy queda al final de la cadena. El rango exacto sale de la red del proxy (por ejemplo `docker network inspect proxy --format '{{range .IPAM.Config}}{{.Subnet}}{{end}}'`).

## Rol admin

La única forma de tener un administrador es `ADMIN_EMAILS`. Es una allow-list de correos y es la **única** fuente del rol `admin`: no existe endpoint que lo conceda ni que lo quite.

Al arrancar, el servidor sincroniza el rol contra la lista —otorga `admin` a las cuentas listadas que aún no lo tienen y se lo revoca a todas las demás— dentro de una transacción. Si la lista queda vacía, se revoca todo. Por eso **basta con quitar un correo de la variable y reiniciar para que esa cuenta pierda el acceso**, sin tocar la base a mano.

Una cuenta nueva con un correo de la lista recibe el rol `admin` en el mismo `POST /auth/register`, sin necesidad de reiniciar. El rol es aditivo: la cuenta sigue siendo compradora (y vendedora si tiene `phone`).

Un administrador no puede suspenderse a sí mismo (`PATCH /admin/users/{id}` responde `400`), para no dejar la plataforma sin nadie que pueda revertir la suspensión.

Las acciones de moderación (suspender, otorgar o revocar vendedor, editar o borrar un auto) se escriben en `admin_audit_log` dentro de la misma transacción que el cambio, y se consultan con `GET /admin/audit`.

Las cuentas se crean con `POST /auth/register` (sin `phone` = solo compradora, con `phone` = compradora + vendedora) y una compradora puede subir a vendedora con `POST /auth/become-seller`.

Qué pasa al arrancar:

1. Lee `.env` si existe (lo que ya está en el shell manda). Si está mal formado, se apaga.
2. Valida `JWT_SECRET` — si falta o es corto, se apaga. Valida `PORT` y `CORS_ALLOWED_ORIGINS`/`ENV`.
3. Abre SQLite con WAL y aplica las migraciones que falten (quedan registradas en `goose_db_version`, nunca borran datos). Si es `:memory:` usa una sola conexión, si no hasta 8 (con `MaxIdleTime` 5 min / `MaxLifetime` 30 min). Si la base está ocupada, reintenta con backoff.
4. Sincroniza el rol `admin` contra `ADMIN_EMAILS` (si viene vacía, revoca todos los administradores).
5. Levanta el HTTP y espera `SIGINT`/`SIGTERM` para apagarse limpio (hasta 10 s).

## Desarrollo

Con `make dev` no necesitas crear `.env`: `scripts/dev.sh` pone valores por defecto (`JWT_SECRET=dev-secret-not-for-production-32b`, `DATABASE_URL=dev.db`) y arranca con `go run`. Si corres `make run` o `go run ./cmd/api` directo sin variables, sí te va a pedir el secreto (es el comportamiento real de producción). `scripts/dev.sh` también rechaza `CORS_ALLOWED_ORIGINS=*` si `ENV=prod`.

Puedes copiar `.env.example` a `.env` para cambiar esos valores.

> `dev.db` y sus archivos (`-wal`, `-shm`) están en `.gitignore`: no los commitees.
