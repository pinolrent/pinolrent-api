# Despliegue

La API corre en un VPS (Contabo) administrado con **[Coolify](https://coolify.io)**, que se ocupa del proxy inverso, los certificados TLS, los deploys desde git, el rollback, los logs y los backups programados. Este documento cubre lo que hay que configurar y por qué.

## Antes de empezar

- VPS con Coolify instalado y el firewall permitiendo solo `22`, `80` y `443`.
- Un dominio con registro **A** apuntando a la IP del VPS: Coolify emite el certificado de Let's Encrypt durante el primer deploy, así que el DNS tiene que estar resuelto antes.
- El repositorio conectado en Coolify (público, o con deploy key si fuera privado).

## Particularidades de esta API

Tres cosas que la diferencian de un backend stateless y que hay que respetar en Coolify:

- **Una sola instancia, siempre.** SQLite con WAL admite muchos lectores pero un solo escritor, y el archivo vive en un volumen local: no hay que escalar la aplicación ni compartir ese volumen entre previews o réplicas.
- **El volumen tiene que ser disco local.** WAL usa `mmap` y locks POSIX, así que nada de NFS ni almacenamiento de red.
- **El proxy de Coolify no es loopback**, y por defecto la API solo cree en `X-Forwarded-For` cuando el proxy viene de la misma máquina. Por eso existe `TRUSTED_PROXY_CIDRS`: sin esa variable, el rate limit por IP queda global (30 logins por minuto entre todos los usuarios) y los logs muestran la IP del proxy en lugar de la del cliente.

## Alta de la aplicación

En Coolify: **+ New → Application → repositorio → Build Pack: Dockerfile**.

| Campo | Valor |
|-------|-------|
| Base Directory | `/` |
| Dockerfile Location | `/Dockerfile` |
| Ports Exposes | `8080` |
| Domain | `https://api.<tu-dominio>` |
| Persistent Storage | **Volume Mount**, nombre `data`, destino `/data` |
| Healthcheck | path `/health`, puerto `8080`, intervalo 30s, start 20s, retries 3 |

El `Dockerfile` compila el binario (estático, sin cgo), lo copia a una imagen Alpine y define `/data` como punto de montaje del volumen persistente: ahí viven la base (`pinolrent.db` con sus `-wal`/`-shm`) y las imágenes de `uploads/`. El `HEALTHCHECK` y la configuración de arriba apuntan al mismo endpoint que ya usa el resto del sistema.

> El entrypoint del contenedor arranca como root, ajusta el dueño del volumen y baja privilegios a un usuario sin permisos. Es necesario porque Docker crea el volumen como root y la app no podría abrir la base.

## Variables de entorno

| Variable | Valor |
|----------|-------|
| `ENV` | `prod` |
| `JWT_SECRET` | `openssl rand -base64 32` en el VPS (marcala como secreta) |
| `DATABASE_URL` | `/data/pinolrent.db` |
| `UPLOAD_DIR` | `/data/uploads` |
| `CORS_ALLOWED_ORIGINS` | origen(es) del frontend, separados por coma |
| `TRUSTED_PROXY_CIDRS` | red del proxy de Coolify (ver abajo) |
| `PORT` | `8080` |

Con `ENV=prod`, `CORS_ALLOWED_ORIGINS=*` es rechazado y el server no arranca: hay que listar los orígenes concretos.

La red del proxy se obtiene en el VPS:

```sh
docker network inspect coolify --format '{{range .IPAM.Config}}{{.Subnet}}{{end}}'
# ej. 10.0.0.0/16
```

Si Coolify recrea esa red, la subred puede cambiar y hay que actualizar la variable. Alternativa estable (aceptable en un VPS de un solo inquilino): confiar en los rangos privados completos, `TRUSTED_PROXY_CIDRS=172.16.0.0/12,10.0.0.0/8`.

Para que `GET /health` informe la versión desplegada, agregar una **build variable** `VERSION` con el tag o el sha del commit; si no, responde `dev`.

## Backups

Son dos capas, porque un archivo a nivel de fichero de una base que está escribiendo puede salir inconsistente (lo advierte la propia documentación de Coolify):

1. **Snapshot consistente (tarea programada)**: en **Applications → Scheduled Tasks**, una tarea diaria a las 03:30 con:

   ```sh
   mkdir -p /data/backups && rm -f "/data/backups/pinolrent-$(date +%F).db" && \
   sqlite3 "$DATABASE_URL" "VACUUM INTO '/data/backups/pinolrent-$(date +%F).db'" && \
   find /data/backups -type f -name '*.db' -mtime +14 -delete
   ```

   `VACUUM INTO` usa el motor de SQLite sobre la base viva: el resultado es una copia íntegra y compacta. La tarea poda los snapshots de más de 14 días.

2. **Archivo del volumen (Coolify → Backups)**: tomar el volumen `data` con frecuencia `daily` y retención de 14 copias / 30 días. Esto archiva los snapshots anteriores más las imágenes de `uploads/`. Dejar **apagado** "Stop containers while creating the archive": el snapshot consistente ya lo hace la tarea diaria, y encenderlo implica downtime todos los días.

**Copia fuera del VPS**: los archivos del punto 2 viven en el disco del propio servidor, así que un disco roto se los lleva. Coolify puede subir cada archivo a un almacenamiento compatible con S3 (Contabo Object Storage, Cloudflare R2, Backblaze B2): se da de alta un *S3 Storage* a nivel de team con endpoint, bucket y credenciales, y después se activa el toggle **S3** en la programación del backup. Quedan dos copias con retención independiente: la local para restaurar rápido y la remota para sobrevivir a la pérdida del servidor.

> Un backup sin restauración probada no es un backup: hacé el ejercicio de la sección de restauración al menos una vez.

## Verificación después de cada deploy

```sh
curl -fsS https://api.<tu-dominio>/health          # {"status":"ok","version":"<tag>"}
curl -sI https://api.<tu-dominio>/health | grep -i strict-transport-security

# el límite por IP tiene que cortar en la ronda 31
for i in $(seq 1 35); do curl -s -o /dev/null -w '%{http_code} ' https://api.<tu-dominio>/auth/login; done

# y un X-Forwarded-For falsificado no debe estrenar bucket
for i in $(seq 1 35); do curl -s -o /dev/null -H "X-Forwarded-For: 10.9.9.$i" \
  -w '%{http_code} ' https://api.<tu-dominio>/auth/login; done
```

**Prueba de persistencia** (es la que valida que el volumen esté bien montado con SQLite): registrar un vendedor con teléfono y subir una imagen, hacer un redeploy desde Coolify y confirmar que el login sigue funcionando y que `GET /uploads/<archivo>` responde `200`.

## Actualización y rollback

1. Push a `main` (o al branch configurado).
2. Deploy en Coolify (automático por webhook o manual). Las migraciones se aplican solas al arrancar y son aditivas.
3. Verificar `/health` con la versión nueva.

Si algo sale mal, el **rollback** de Coolify vuelve a la deployment anterior. Mientras las migraciones sigan siendo aditivas, un binario viejo funciona contra el esquema nuevo. Conviene además activar las notificaciones de deploy fallido en Coolify (Discord/Telegram/email) para enterarse sin depender de un usuario.

## Restauración

Coolify no restaura los backups de storage desde el panel: el archivo se descarga y se aplica a mano.

1. Descargar el `.tar.gz` de la ejecución de backup (o el snapshot diario directamente del volumen).
2. Descomprimir y quedarse con `pinolrent-<fecha>.db` de `/data/backups/`, que es la copia consistente.
3. Comprobar que sirve:

   ```sh
   sqlite3 pinolrent-2026-09-18.db "PRAGMA integrity_check;"
   sqlite3 pinolrent-2026-09-18.db "SELECT count(*) FROM users;"
   ```

4. Con el contenedor detenido, reemplazar `/data/pinolrent.db` por esa copia y borrar los `pinolrent.db-wal` / `pinolrent.db-shm` viejos (son de la base anterior).
5. Arrancar de nuevo y verificar `/health` y que los datos estén.

## Limitaciones conocidas

- Con snapshot diario se puede perder hasta 24 h de datos si el servidor muere; la copia a S3 no cambia eso, solo evita perderlo todo.
- `uploads/` crece sin límite: no hay limpieza de imágenes huérfanas, así que conviene mirar el disco cada tanto.
- El `WriteTimeout` del server es de 30 s, que en conexiones móviles lentas puede cortar una subida de 5 MB.
- El rate limit es en memoria y por proceso: con una sola instancia es correcto, pero reiniciar el contenedor lo resetea.
