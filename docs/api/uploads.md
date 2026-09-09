# Subir archivos

## `POST /uploads`

Sube una imagen y devuelve su ruta local. Necesita login (cualquier rol). La ruta devuelta se usa luego como `photo_url` en `POST /seller/cars` o `proof_url` en `POST /reservations/{id}/payment`.

**Body:** `multipart/form-data` con el campo `file`.

**Responde** `201`:

```json
{"url":"/uploads/3f9a1c2e4b5d6f708192a3b4c5d6e7f8.jpg"}
```

Reglas: solo `jpg`, `png` o `webp` (se detecta por contenido, no por extensión), máximo **5 MB** por archivo. El nombre se genera en el server, se ignora el original.

**Errores:**

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `file is required` | Falta el campo `file` |
| `400` | `invalid multipart body` | Body no es multipart válido |
| `413` | `request body too large` | Más de 5 MB |
| `415` | `only jpg, png or webp images are allowed` | No es imagen soportada |
| `401` | ver [00-general](00-general.md) | Sin token |

---

## `GET /uploads/{nombre}`

Devuelve la imagen. No necesita login. Responde `404 {"error":"upload not found"}` si no existe, es un directorio o la extensión no es de imagen. Con `Cache-Control: public, max-age=31536000, immutable`.

Ejemplo de flujo:

```sh
url=$(curl -s -F "file=@foto.jpg" -H "Authorization: Bearer $TOKEN" \
  "$BASE/uploads" | jq -r .url)
curl -s -X POST "$BASE/seller/cars" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"Toyota Yaris\",\"price_per_day\":45000,\"photo_url\":\"$url\"}"
```

Las imágenes viven en el directorio `UPLOAD_DIR` (por defecto `uploads/`). Ver [configuración](../configuracion.md).
