# Admin

Endpoints de administración de la plataforma. Todos exigen un token de una
cuenta con el rol `admin`.

## Cómo se consigue el rol `admin`

Por la variable de entorno `ADMIN_EMAILS`, y por nada más. Es una allow-list
de correos y el servidor la sincroniza contra la base al arrancar:

- las cuentas listadas que no lo tienen, lo reciben;
- las cuentas que lo tienen y no están en la lista, lo pierden;
- una lista vacía revoca el rol a todas.

Una cuenta que se registra con un correo de la lista nace ya como `admin`, sin
esperar un reinicio. **No existe endpoint para conceder ni para quitar el rol
`admin`** — es deliberado: si lo hubiera, cualquiera con un token de
administrador sería además el camino para mantenerse administrador por siempre,
y la allow-list dejaría de ser la fuente de verdad.

Ver [`docs/configuracion.md`](../configuracion.md) para el detalle.

## Autorización

Cada endpoint responde:

| Situación | Código |
|-----------|--------|
| Sin `Authorization: Bearer` | `401` |
| Token inválido o expirado | `401` |
| Cuenta suspendida | `403` `account suspended` |
| Token válido sin rol `admin` | `403` `insufficient permissions` |

Un administrador **no** es vendedor implícito: los endpoints `/seller/*` siguen
exigiendo el rol `seller`. La administración va por `/admin/*`.

Las lecturas (`GET`) no llevan rate limit; las que escriben (`PATCH`,
`DELETE`) usan el bucket estándar por IP.

---

## `GET /admin/users`

Lista las cuentas de la plataforma. Admite filtros y paginación.

**Query:**

| Parámetro | Tipo | Reglas |
|-----------|------|--------|
| `q` | texto | busca en `email` (subcadena, sin distinguir mayúsculas) o en el id exacto |
| `role` | texto | `buyer`, `seller` o `admin` |
| `limit` | número | 1 a 200; default 50 |
| `offset` | número | 0 a 10000; default 0 |

```json
{
  "items": [
    {"id": 1, "email": "admin@example.com", "roles": ["admin", "buyer"], "phone": ""},
    {"id": 2, "email": "vendedor@example.com", "phone": "+56912345678", "roles": ["buyer", "seller"]},
    {"id": 3, "email": "suspendido@example.com", "roles": ["buyer"], "suspended_at": 1730000000}
  ],
  "total": 3,
  "limit": 50,
  "offset": 0
}
```

`phone` viene vacío si la cuenta no tiene. `suspended_at` (Unix) solo aparece
en cuentas suspendidas. `roles` viene siempre ordenado alfabéticamente.

**Errores:**

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid limit` | `limit` no es número o está fuera de 1..200 |
| `400` | `invalid offset` | `offset` es negativo o mayor a 10000 |

---

## `GET /admin/users/{id}`

Detalle de una cuenta: id, email, phone, roles y `suspended_at` si está
suspendida. Misma forma que un elemento de la lista.

**Errores:**

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid user id` | `{id}` no es un entero |
| `404` | `user not found` | No existe la cuenta |

---

## `PATCH /admin/users/{id}`

Suspende o reactiva una cuenta. Una cuenta suspendida recibe `403 account
suspended` en **todas** las rutas autenticadas, con un token que siga siendo
válido. Al reactivarla recupera el acceso sin volver a loguearse.

**Body:**

| Campo | Tipo | ¿Obligatorio? | Reglas |
|-------|------|---------------|--------|
| `suspended` | booleano | sí | `true` suspende, `false` reactiva |

```json
{"suspended": true}
```

**Responde** `200` con el estado actualizado de la cuenta.

La operación es idempotente: suspender una cuenta ya suspendida devuelve `200`
sin escribir nada ni en la base ni en la auditoría.

Un administrador **no puede suspenderse a sí mismo** (`400 cannot suspend
yourself`): sería la forma de dejar la plataforma sin nadie que pueda revertir
la suspensión.

**Errores:**

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid user id` | `{id}` no es un entero |
| `400` | `cannot suspend yourself` | `{id}` es la cuenta que hace la llamada |
| `400` | `suspended is required` | Falta `suspended` en el body |
| `404` | `user not found` | No existe la cuenta |

---

## `PATCH /admin/users/{id}/roles`

Otorga o revoca el rol `seller`. No toca el rol `admin` (ver arriba).

**Body:**

| Campo | Tipo | ¿Obligatorio? | Reglas |
|-------|------|---------------|--------|
| `seller` | booleano | sí | `true` otorga vendedor, `false` lo revoca |

```json
{"seller": true}
```

**Responde** `200` con los roles resultantes. Es idempotente: otorgar a quien
ya es vendedor devuelve `200` sin escribir.

Revocar el rol `seller` no borra los autos ni las reservas de la cuenta: solo
le quita el acceso a `/seller/*`.

**Errores:**

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid user id` | `{id}` no es un entero |
| `400` | `seller is required` | Falta `seller` en el body |
| `404` | `user not found` | No existe la cuenta |

---

## `GET /admin/cars`

Lista todos los autos de la plataforma, **incluidos los inactivos**. No aplica
el filtro de propietario ni el de disponibilidad por fechas de la vista
pública: es una vista administrativa.

**Query:** `q` (nombre o id), `owner_id`, `active` (`true`/`false`), `limit`,
`offset`.

```json
{
  "items": [
    {"id": 1, "owner_id": 2, "name": "Toyota Yaris", "price_per_day": 45000,
     "active": true, "owner_email": "vendedor@example.com"}
  ],
  "total": 1,
  "limit": 50,
  "offset": 0
}
```

**Errores:** `400` con `invalid limit`, `invalid offset`, `invalid owner_id`
o `invalid active`.

---

## `PATCH /admin/cars/{id}`

Edita cualquier auto, sin importar su propietario. Acepta los mismos campos
que `PATCH /seller/cars/{id}` (`name`, `photo_url`, `price_per_day`, `active`),
al menos uno.

La diferencia con la ruta del vendedor: **el administrador puede desactivar un
auto que tiene reservas futuras**. El vendedor no puede (recibiría `409`),
porque desactivarse a sí mismo le rompería los contratos abiertos; el
administrador sí, porque deskusar un anuncio problemático es justamente el caso
de uso.

**Responde** `200 {"status":"ok"}`.

**Errores:**

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid car id` | `{id}` no es un entero |
| `400` | `no fields to update` | Body vacío de campos conocidos |
| `400` | *(mensaje de validación)* | Nombre, precio o foto inválidos |
| `404` | `car not found` | No existe el auto |

---

## `DELETE /admin/cars/{id}`

Borra cualquier auto sin historial de reservas, sin importar su propietario.

Un auto que tiene reservas **no** se borra: responde `409`. Las filas de
reservas referencian al auto, así que un borrado rompería el historial. Para
sacarlo de la vista pública hay que desactivarlo (`PATCH` con `active: false`),
que es lo que hace la ruta del vendedor también.

**Responde** `200 {"status":"ok"}`.

**Errores:** `400 invalid car id`, `404 car not found`, `409 car has reservations,
cannot delete`.

---

## `GET /admin/reservations`

Lista todas las reservas, con `buyer_email` y `car_name` resueltos para no
tener que encadenar llamadas.

**Query:** `status` (`pending`, `confirmed`, `cancelled`), `user_id`,
`car_id`, `limit`, `offset`.

```json
{
  "items": [
    {"id": 1, "user_id": 3, "car_id": 1, "start_date": "2026-10-05",
     "end_date": "2026-10-07", "status": "confirmed",
     "buyer_email": "comprador@example.com", "car_name": "Toyota Yaris"}
  ],
  "total": 1,
  "limit": 50,
  "offset": 0
}
```

---

## `GET /admin/payments`

Lista todos los pagos, con el contexto de la reserva y del comprador.

**Query:** `status` (`pending`, `approved`, `rejected`), `reservation_id`,
`limit`, `offset`.

```json
{
  "items": [
    {"id": 1, "reservation_id": 1, "method": "pos", "status": "approved",
     "proof_url": "uploads/x.png", "user_id": 3, "car_id": 1,
     "buyer_email": "comprador@example.com"}
  ],
  "total": 1,
  "limit": 50,
  "offset": 0
}
```

---

## `GET /admin/stats`

Métricas de la plataforma, en una sola llamada. Solo lectura, sin paginación.

```json
{
  "users": {"total": 12, "sellers": 4, "admins": 1, "suspended": 1},
  "cars": {"active": 9},
  "reservations": {"pending": 3, "confirmed": 5, "cancelled": 2},
  "payments": {"pending": 1, "approved": 4, "rejected": 1, "approved_total": 540000}
}
```

Una reserva no tiene estado `paid`: el CHECK de la tabla solo admite
`pending`, `confirmed` y `cancelled`. Que una reserva esté pagada se deduce de
que su pago esté `approved`, y eso es lo que cuenta `payments.approved`.

`approved_total` suma `price_per_day` × días de cada reserva con pago
aprobado. Ojo: las reservas guardan fechas, no precio, así que la cifra usa el
**precio actual** del auto, no el que estaba pactado al reservar. Es una
aproximación para dimensionar la plataforma, no un libro de ingresos.

---

## `GET /admin/audit`

Historial de acciones administrativas. Cada fila dice quién hizo qué, sobre
qué, y cuándo. Los índices son por `actor_id` y por `action`, y el orden es
del más reciente al más antiguo.

**Query:** `actor_id`, `action`, `limit`, `offset`.

```json
{
  "items": [
    {"id": 12, "actor_id": 1, "actor_email": "admin@example.com",
     "action": "user.suspend", "target_type": "users", "target_id": 3,
     "created_at": "2026-10-03 15:12:04"}
  ],
  "total": 1,
  "limit": 50,
  "offset": 0
}
```

### Acciones auditadas

| `action` | Origen | Qué registra |
|----------|--------|--------------|
| `user.suspend` | `PATCH /admin/users/{id}` | Cuenta suspendida |
| `user.unsuspend` | `PATCH /admin/users/{id}` | Cuenta reactivada |
| `user.role_grant_seller` | `PATCH /admin/users/{id}/roles` | Vendedor otorgado |
| `user.role_revoke_seller` | `PATCH /admin/users/{id}/roles` | Vendedor revocado |
| `car.update` | `PATCH /admin/cars/{id}` | Auto editado o desactivado |
| `car.delete` | `DELETE /admin/cars/{id}` | Auto borrado |

Cada escritura va **dentro de la misma transacción** que el cambio que la
origina: o quedan las dos cosas, o no queda ninguna. Por eso una operación
idempotente que no cambia nada tampoco agrega una fila al historial.

Las lecturas administrativas (`GET`) no se auditan: llenan el registro de ruido
sin registrar ninguna decisión.

El registro sobrevive a la cuenta que lo hizo: `actor_id` no tiene cascada de
borrado. Si algún día se borrara el usuario, el `actor_id` quedaría apuntando
a una cuenta inexistente y por eso la consulta lo resuelve con `LEFT JOIN`
(`actor_email` puede venir vacío).