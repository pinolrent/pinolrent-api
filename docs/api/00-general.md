# Convenciones de la API

La base de la API es `http://localhost:8080` en desarrollo (`PORT`).

## Formato

- **JSON** en requests y responses (`Content-Type: application/json`).
- Las fechas son strings ISO `YYYY-MM-DD` (texto, UTC para la comparación con "hoy").
- `price_per_day` es un entero en **centavos**.

## Autenticación

Las rutas protegidas requieren:

```
Authorization: Bearer <token>
```

El token se obtiene de `POST /auth/login` (ver [auth](auth.md)) y vence a las
**24 h**. Los roles: `client` (rutas de reservas y pagos) y `admin` (rutas de
admin).

Errores de autenticación:

| Status | Mensaje | Cuándo |
|--------|---------|--------|
| `401` | `missing bearer token` | Sin cabecera `Authorization` o sin prefijo `Bearer` |
| `401` | `invalid or expired token` | Token inválido o vencido |
| `401` | `user not found` | El `uid` del token no existe en la base |
| `403` | `insufficient permissions` | Token válido pero rol equivocado para la ruta |

## Errores

Todos los errores siguen el mismo shape:

```json
{"error":"mensaje"}
```

- Los **400** se usan para validación de entrada y JSON inválido.
- Los **409** se usan para conflictos de dominio (duplicado, overlap, estado inválido).
- Los **404** ocultan recursos ajenos: un cliente nunca sabe si la reserva de otro
  existe (devuelve `404` igual).

## Límites y rate limiting

- **Body máx. 1 MB** por request. JSON estricto: campos desconocidos o payload
  malformado → `400 {"error":"invalid JSON body"}`; exceso → `413
  {"error":"request body too large"}`.
- **Rate limit por IP** (token bucket en memoria, `RemoteAddr` sin puerto) solo
  en rutas cuyo path empieza por `/auth/`: **30 req/60 s** con ráfaga de 30.
  Sobre el límite → `429 {"error":"too many requests"}`.

## Índice de endpoints

| Método | Ruta | Auth | Documento |
|--------|------|------|-----------|
| GET | `/health` | no | [auth](auth.md) |
| POST | `/auth/register` | no | [auth](auth.md) |
| POST | `/auth/login` | no | [auth](auth.md) |
| GET | `/cars` | no | [cars](cars.md) |
| POST | `/admin/cars` | admin | [cars](cars.md) |
| PATCH | `/admin/cars/{id}` | admin | [cars](cars.md) |
| POST | `/reservations` | client | [reservations](reservations.md) |
| GET | `/reservations` | client | [reservations](reservations.md) |
| GET | `/reservations/{id}` | client/admin | [reservations](reservations.md) |
| POST | `/reservations/{id}/payment` | client | [payments](payments.md) |
| PATCH | `/admin/reservations/{id}/confirm` | admin | [payments](payments.md) |

## Tipos compartidos

**Car:**

```json
{"id":1,"name":"Toyota Yaris","photo_url":"https://...","price_per_day":45000,"active":true}
```

`photo_url` se omite cuando está vacío.

**Reservation view** (reserva con su auto y, si existe, su pago):

```json
{
  "id":1,
  "user_id":3,
  "car_id":1,
  "start_date":"2026-10-01",
  "end_date":"2026-10-05",
  "status":"pending",
  "car":{ "...": "Car" },
  "payment":{ "...": "Payment" }
}
```

`payment` se omite mientras no exista.

**Payment:**

```json
{"id":1,"reservation_id":1,"method":"pos","status":"pending","proof_url":"https://..."}
```

`proof_url` se omite cuando está vacío.