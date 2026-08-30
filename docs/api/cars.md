# Autos

## `GET /cars`

Lista los autos **activos** de todos los vendedores, opcionalmente excluyendo
los ya reservados en un rango. Público.

**Query params** (opcionales, van juntos o ninguno):

| Parámetro | Formato | Reglas |
|-----------|---------|--------|
| `start_date` | `YYYY-MM-DD` | debe acompañarse de `end_date` |
| `end_date` | `YYYY-MM-DD` | `>= start_date` |

¿Como funciona el filtro? Solo se listan autos con `active=1` y sin reserva
`pending`/`confirmed` que **solape** el rango `[start_date, end_date]`. Las
reservas `cancelled` no bloquean.

**Respuesta** — `200 OK`:

```json
[
  {"id":1,"owner_id":4,"name":"Toyota Yaris","photo_url":"https://...","price_per_day":45000,"active":true},
  {"id":2,"owner_id":7,"name":"Fiat Cronos","price_per_day":38000,"active":true}
]
```

Sin resultados → `[]`.

**Errores:**

| Status | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `start_date and end_date must be provided together` | Solo se mandó uno de los dos |
| `400` | `invalid start_date, expected YYYY-MM-DD` | Formato inválido |
| `400` | `invalid end_date, expected YYYY-MM-DD` | Formato inválido |
| `400` | `end_date must be on or after start_date` | Rango invertido |

---

## `GET /seller/cars`

Lista los autos del vendedor autenticado, más nuevos primero. Requiere rol
`seller`.

**Respuesta** — `200 OK`:

```json
[
  {"id":1,"owner_id":4,"name":"Toyota Yaris","price_per_day":45000,"active":true}
]
```

Sin autos → `[]`.

---

## `POST /seller/cars`

Da de alta un auto **propio**. Requiere rol `seller`.

**Body:**

| Campo | Tipo | Requerido | Reglas |
|-------|------|-----------|--------|
| `name` | string | sí | no vacío (trim) |
| `photo_url` | string | no | si se manda, URL `http(s)` válida |
| `price_per_day` | entero | no | `0..100_000_000` centavos |

```json
{"name":"Toyota Yaris","photo_url":"https://example.com/yaris.jpg","price_per_day":45000}
```

**Respuesta** — `201 Created` (nace `active: true`, con tu `owner_id`):

```json
{"id":1,"owner_id":4,"name":"Toyota Yaris","photo_url":"https://example.com/yaris.jpg","price_per_day":45000,"active":true}
```

**Errores:**

| Status | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `name is required` | `name` vacío o ausente |
| `400` | `price_per_day must be >= 0` | Precio negativo |
| `400` | `price_per_day must be <= 100000000` | Precio sobre el tope |
| `400` | `invalid photo_url` | URL malformada o sin scheme `http(s)` |
| `401` / `403` | ver [00-general](00-general.md) | Sin token / rol incorrecto |

---

## `PATCH /seller/cars/{id}`

Activa/inactiva un auto **propio**. Requiere rol `seller`.

**Body** (parcial; solo `active`):

```json
{"active":false}
```

**Respuesta** — `200 OK` (el auto completo):

```json
{"id":1,"owner_id":4,"name":"Toyota Yaris","photo_url":"https://example.com/yaris.jpg","price_per_day":45000,"active":false}
```

**Errores:**

| Status | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `invalid car id` | `{id}` no es un entero |
| `400` | `active is required` | Campo ausente (debe ser `true`/`false` explícito) |
| `404` | `car not found` | No existe, **o** pertenece a otro vendedor (se oculta) |
| `401` / `403` | ver [00-general](00-general.md) | Sin token / rol incorrecto |

---

**Nota:** inactivar un auto no borra sus reservas pasadas; simplemente deja de
aparecer en `GET /cars` y de aceptar reservas nuevas (`409 car is not active`).