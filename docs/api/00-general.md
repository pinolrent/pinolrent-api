# Cómo usar la API

La API está en `http://localhost:8080` en desarrollo (cambia con `PORT`).

## Formato

- Todo es **JSON** (`Content-Type: application/json`).
- Fechas como texto `YYYY-MM-DD` (se comparan en UTC).
- `price_per_day` es un número entero en **centavos**.

## Cómo autenticarte

Las rutas protegidas necesitan:

```
Authorization: Bearer <token>
```

El token lo sacas de `POST /auth/login`: el access dura **15 min** y el refresh **7 días** (se renueva con `POST /auth/refresh`).

Roles:

- **`buyer`** (comprador): reserva, paga y ve sus reservas.
- **`seller`** (vendedor): publica sus autos y confirma reservas de sus autos. Se crea con `POST /auth/register/seller`.

Un vendedor también puede reservar como comprador.

Si algo falla con el login:

| Código | Mensaje | Cuándo pasa |
|--------|---------|-------------|
| `401` | `missing bearer token` | No mandaste el header o sin `Bearer` |
| `401` | `invalid or expired token` | Token trucho, vencido o revocado |
| `401` | `user not found` | El usuario del token ya no existe |
| `403` | `insufficient permissions` | Token válido pero sin permiso para esa ruta |

## Cómo responde errores

Siempre así:

```json
{"error":"mensaje"}
```

- `400` para datos mal mandados o JSON inválido.
- `409` para choques (fechas ocupadas, estado que no permite la acción, pago duplicado). El email duplicado en registro responde el mismo `201 {"email":"..."}` a propósito para no revelar qué emails existen.
- `404` para ocultar lo ajeno: si no es tuyo, responde como si no existiera.

## Límites

- **Body máximo 1 MB.** JSON estricto: si mandas campos que no existen o JSON roto → `400 {"error":"invalid JSON body"}`; si te pasas del tamaño → `413`.
- **Límite por IP**: `/auth/*` → **30 por minuto** (ráfaga 30); escritura (`POST /reservations`, `POST /seller/cars`, `POST /reservations/*/payment`) → **120 por minuto** (ráfaga 20). Si te pasas → `429 {"error":"too many requests"}`. Respeta `X-Forwarded-For` / `X-Real-IP`.
- **CORS** abierto a todos por defecto (`CORS_ALLOWED_ORIGINS=*`), con `ENV=prod` o `production` es rechazado. En producción poné tus orígenes separados por coma, ej. `CORS_ALLOWED_ORIGINS=https://app.example.com`. Los preflights `OPTIONS` responden `204`; si el origen no está permitido, no lleva `Access-Control-Allow-Origin` y el navegador lo bloquea. Solo `GET`, `POST`, `PATCH`, `OPTIONS` y headers `Authorization`, `Content-Type`. Trailing `/` en origen es tolerado.

## Paginación

Las listas (`GET /cars`, `GET /seller/cars`, `GET /reservations`, `GET /seller/reservations`) aceptan:

| Parámetro | Por defecto | Reglas |
|-----------|-------------|--------|
| `limit` | `50` | `1..200`; si no → `400 "invalid limit"` |
| `offset` | `0` | `0..10000`; si no → `400 "invalid offset"` |

Responden un **array simple**. Para saber si hay más, pedí `limit+1` y fijate si vino uno extra.

## Endpoints

| Método | Ruta | ¿Login? | Doc |
|--------|------|---------|-----|
| GET | `/health` | no | [auth](auth.md) |
| POST | `/auth/register` | no | [auth](auth.md) |
| POST | `/auth/register/seller` | no | [auth](auth.md) |
| POST | `/auth/login` | no | [auth](auth.md) |
| GET | `/auth/me` | sí | [auth](auth.md) |
| POST | `/auth/logout` | sí | [auth](auth.md) |
| GET | `/cars` | no | [cars](cars.md) |
| GET | `/cars/{id}` | no | [cars](cars.md) |
| GET | `/seller/cars` | vendedor | [cars](cars.md) |
| POST | `/seller/cars` | vendedor | [cars](cars.md) |
| PATCH | `/seller/cars/{id}` | vendedor | [cars](cars.md) |
| POST | `/reservations` | sí | [reservations](reservations.md) |
| GET | `/reservations` | sí | [reservations](reservations.md) |
| GET | `/reservations/{id}` | sí | [reservations](reservations.md) |
| PATCH | `/reservations/{id}/cancel` | sí | [reservations](reservations.md) |
| POST | `/reservations/{id}/payment` | sí | [payments](payments.md) |
| GET | `/seller/reservations` | vendedor | [payments](payments.md) |
| PATCH | `/seller/reservations/{id}/confirm` | vendedor | [payments](payments.md) |

## Formas que devuelve la API

**Auto:**

```json
{"id":1,"owner_id":4,"name":"Toyota Yaris","photo_url":"https://...","price_per_day":45000,"active":true}
```

`photo_url` no aparece si está vacío. `owner_id` es quién lo publicó.

**Reserva (con su auto y su pago si existe):**

```json
{
  "id":1,
  "user_id":3,
  "car_id":1,
  "start_date":"2026-10-01",
  "end_date":"2026-10-05",
  "status":"pending",
  "car":{ "...": "auto" },
  "payment":{ "...": "pago" }
}
```

`payment` no aparece hasta que se paga.

**Pago:**

```json
{"id":1,"reservation_id":1,"method":"pos","status":"pending","proof_url":"https://..."}
```

`proof_url` no aparece si está vacío.
