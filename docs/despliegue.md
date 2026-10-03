# Despliegue

Notas de operación: qué restricciones impone este backend y cómo se respalda y restaura. El despliegue en sí es cualquier forma de correr el binario o el contenedor detrás de un proxy con TLS.

## Restricciones del proyecto

- **Una sola instancia, siempre.** SQLite con WAL admite muchos lectores pero un solo escritor, y el archivo vive en un volumen local: no escalar la aplicación ni compartir ese volumen entre previews o réplicas.
- **Disco local, nada de red.** WAL usa `mmap` y locks POSIX, así que `DATABASE_URL` no puede apuntar a NFS ni a almacenamiento de red.
- **Detrás de un proxy que no es loopback** (nginx/Caddy en otra red, contenedores, cualquier PaaS) hay que declarar esa red en `TRUSTED_PROXY_CIDRS`, o todos los clientes comparten un mismo bucket de límite (30 logins por minuto entre todos) y los logs registran la IP del proxy en lugar de la del cliente. Ejemplo para obtener la subred de una red de contenedores:

```sh
docker network inspect proxy --format '{{range .IPAM.Config}}{{.Subnet}}{{end}}'
# ej. 10.0.0.0/16
```

Ver [Configuración](configuracion.md) para cómo se recorre la cadena de `X-Forwarded-For`.

## Variables en producción

| Variable | Valor |
|----------|-------|
| `ENV` | `prod` |
| `JWT_SECRET` | `openssl rand -base64 32`, marcado como secreto y fuera del repo |
| `DATABASE_URL` | archivo SQLite en el volumen persistente, ej. `/data/pinolrent.db` |
| `UPLOAD_DIR` | directorio persistente de imágenes, ej. `/data/uploads` |
| `CORS_ALLOWED_ORIGINS` | origen(es) del frontend, separados por coma (`*` se rechaza con `ENV=prod`) |
| `TRUSTED_PROXY_CIDRS` | red del proxy inverso (ver arriba) |
| `PORT` | `8080` |

El contenedor debe montar `/data` como volumen persistente: ahí viven la base (`pinolrent.db` con sus `-wal`/`-shm`) y `uploads/`. El healthcheck apunta a `GET /health` (puerto 8080). La versión que informa la imagen la calcula sola con `git describe` sobre el clone (tag + sha); se puede sobrescribir con el build-arg `VERSION`, y responde `dev` solo si el build no tiene `.git`.

### Primer administrador

El rol `admin` no se da de alta por API. Corré el comando contra la misma base (una vez):

```sh
DATABASE_URL=/data/pinolrent.db go run ./cmd/admin -email admin@tudominio.com -password '...'
```

Crea la cuenta si no existe (nace solo con rol `admin`) y, si ya existe, solo le garantiza el rol sin tocar la contraseña. Es idempotente.

## Deploy y rollback

1. Push a `main`: la plataforma de despliegue, con el auto-deploy (webhook) conectado al repo, reconstruye la imagen y reemplaza el contenedor. Las migraciones se aplican solas al arrancar y son aditivas.
2. Verificar: `curl -fsS https://api.<dominio>/health` → `{"status":"ok","version":"v0.1.0-N-g<sha>"}` — la versión debe corresponder al commit recién pusheado; si dice `dev`, el build no tuvo acceso a `.git`.
3. Si algo sale mal, redeploy del commit anterior desde la plataforma y repetir la verificación. Mientras las migraciones sigan siendo aditivas, un binario viejo funciona contra el esquema nuevo.

Los releases se tagean con `vX.Y.Z` (`git tag -a vX.Y.Z -m "..."`). Entre tags, la versión reportada combina el último tag con el sha: `v0.1.0-3-g2627c64`.

## Backups

Un archivo a nivel de fichero de una base que está escribiendo puede salir inconsistente, así que son dos capas:

1. **Snapshot consistente** (tarea diaria): `VACUUM INTO` usa el motor de SQLite sobre la base viva — el resultado es una copia íntegra y compacta. La tarea poda los snapshots de más de 14 días:

   ```sh
   mkdir -p /data/backups && rm -f "/data/backups/pinolrent-$(date +%F).db" && \
   sqlite3 "$DATABASE_URL" "VACUUM INTO '/data/backups/pinolrent-$(date +%F).db'" && \
   find /data/backups -type f -name '*.db' -mtime +14 -delete
   ```

2. **Copia off-site**: los snapshots y `uploads/` viven en el disco del propio servidor, así que un disco roto se los lleva. Subilos a cualquier almacenamiento compatible con S3 (Object Storage, R2, B2…) con retención independiente: la copia local para restaurar rápido, la remota para sobrevivir a la pérdida del servidor.

> Un backup sin restauración probada no es un backup: hacé el ejercicio de la sección siguiente al menos una vez.

## Restauración

1. Descargar la copia (el archivo del backup o el snapshot diario del volumen).
2. Quedarse con el `.db` de `/data/backups/`, que es la copia consistente.
3. Comprobar que sirve:

   ```sh
   sqlite3 pinolrent-<fecha>.db "PRAGMA integrity_check;"
   sqlite3 pinolrent-<fecha>.db "SELECT count(*) FROM users;"
   ```

4. Con el proceso detenido, reemplazar la base por esa copia y borrar los `-wal` / `-shm` viejos (son de la base anterior).
5. Arrancar de nuevo y verificar `/health` y que los datos estén.

## Limitaciones conocidas

- Con snapshot diario se puede perder hasta 24 h de datos si el servidor muere; la copia off-site no cambia eso, solo evita perderlo todo.
- `uploads/` tiene cuota total (`UPLOAD_MAX_TOTAL_MB`, default 1 GB) y un sweep cada 6 horas borra los archivos huérfanos con más de 7 días; aun así conviene mirar el disco cada tanto.
- El `WriteTimeout` es de 120 s: una subida de 5 MB sobrevive desde ~37 KB/s, pero en enlaces realmente miserables aún puede cortar.
- El rate limit es en memoria y por proceso: con una sola instancia es correcto, pero reiniciar el proceso lo resetea.
