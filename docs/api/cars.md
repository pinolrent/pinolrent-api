# Autos

## `GET /cars`

Lista autos **activos** de todos los vendedores. Puedes filtrar por vendedor y por fechas (excluye los ya reservados en ese rango). No necesita login.

**Filtros** (todos opcionales, se pueden combinar):

| Parámetro | Formato | Reglas |
|-----------|---------|--------|
| `start_date` | `YYYY-MM-DD` | debe ir con `end_date` |
| `end_date` | `YYYY-MM-DD` | `>= start_date` |
| `owner_id` | número | solo autos de ese vendedor |
| `limit` | número | `1..200`, por defecto `50` |
| `offset` | número | `>= 0`, por defecto `0` |

Solo muestra autos con `active=1` y sin reserva `pending`/`confirmed` que choque con `[start_date, end_date]`. Las `cancelled` no bloquean.

**Responde** `200`:

```json
[
  {"id":1,"owner_id":4,"name":"Toyota Yaris","photo_url":"https://...","price_per_day":45000,"active":true},
  {"id":2,"owner_id":7,"name":"Fiat Cronos","price_per_day":38000,"active":true}
]
```

Sin resultados → `[]`.

**Errores:**

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `start_date and end_date must be provided together` | Mandaste solo uno |
| `400` | `invalid start_date, expected YYYY-MM-DD` | Formato mal |
| `400` | `invalid end_date, expected YYYY-MM-DD` | Formato mal |
| `400` | `end_date must be on or after start_date` | Rango al revés |
| `400` | `invalid owner_id` | No es un número válido |
| `400` | `invalid limit` / `invalid offset` | Paginación mal |

---

## `GET /cars/{id}`

Detalle de un auto **activo**. No necesita login.

**Responde** `200`:

```json
{"id":1,"owner_id":4,"name":"Toyota Yaris","photo_url":"https://...","price_per_day":45000,"active":true}
```

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid car id` | `{id}` no es número |
| `404` | `car not found` | No existe o está inactivo |

---

## `GET /seller/cars`

Tus autos como vendedor, más nuevos primero. Necesita ser `seller`. Acepta `limit`/`offset`.

```json
[{"id":1,"owner_id":4,"name":"Toyota Yaris","price_per_day":45000,"active":true}]
```

Sin autos → `[]`.

---

## `POST /seller/cars`

Agrega un auto tuyo. Necesita ser `seller`.

**Body:**

| Campo | Tipo | ¿Obligatorio? | Reglas |
|-------|------|---------------|--------|
| `name` | texto | sí | no vacío, hasta 200 caracteres |
| `photo_url` | texto | no | si va, URL `http(s)` hasta 2048 |
| `price_per_day` | número | no | `0..100_000_000` centavos |

```json
{"name":"Toyota Yaris","photo_url":"https://example.com/yaris.jpg","price_per_day":45000}
```

**Responde** `201` (nace `active: true` con tu `owner_id`):

```json
{"id":1,"owner_id":4,"name":"Toyota Yaris","photo_url":"https://example.com/yaris.jpg","price_per_day":45000,"active":true}
```

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `name is required` | Vacío o falta |
| `400` | `name is too long (max 200 characters)` | Más de 200 |
| `400` | `price_per_day must be >= 0` | Negativo |
| `400` | `price_per_day must be <= 100000000` | Se pasó del tope |
| `400` | `photo_url is too long` | Más de 2048 |
| `400` | `invalid photo_url` | URL mal formada o sin `http(s)` |
| `400` | `invalid JSON body` | JSON roto o campos desconocidos |
| `413` | `request body too large` | Más de 1 MB |
| `401` / `403` | ver [00-general](00-general.md) | Sin token o sin permiso |

---

## `PATCH /seller/cars/{id}`

Prende o apaga uno de tus autos. Necesita ser `seller`.

```json
{"active":false}
```

**Responde** `200` con el auto actualizado:

```json
{"id":1,"owner_id":4,"name":"Toyota Yaris","photo_url":"https://example.com/yaris.jpg","price_per_day":45000,"active":false}
```

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid car id` | `{id}` no es número |
| `400` | `active is required` | Falta el campo (debe ser `true` o `false`) |
| `404` | `car not found` | No existe o no es tuyo |
| `401` / `403` | ver [00-general](00-general.md) | Sin token o sin permiso |

---

> Apagar un auto no borra sus reservas viejas, solo deja de aparecer en `GET /cars` y no acepta reservas nuevas (`409 car is not active`).
