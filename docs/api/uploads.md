# Subir archivos

## `POST /uploads`

Sube una imagen y devuelve su ruta local. Necesita login (cualquier rol). La ruta devuelta se usa luego como `photo_url` en `POST /seller/cars` o `proof_url` en `POST /reservations/{id}/payment`.

**Body:** `multipart/form-data` con el campo `file`.

**Responde** `201`:

```json
{"url":"/uploads/3f9a1c2e4b5d6f708192a3b4c5d6e7f8.jpg"}
```

Reglas: solo `jpg`, `png` o `webp` (se detecta por contenido, no por extensión), máximo **5 MB** por archivo. El nombre se genera en el server, se ignora el original.

Las imágenes `jpg` y `png` se **re-codifican** en el server (jpeg calidad 85, png sin pérdida): el archivo guardado son solo píxeles, así que los metadatos EXIF —GPS, modelo de cámara, fecha— no llegan nunca al disco ni a la respuesta pública. Esto importa: la foto de un auto suele traer las coordenadas de la casa del vendedor. `webp` se guarda tal cual, con sus metadatos (la stdlib de Go no trae encoder webp).

Como la re-codificación tiene que decodificar la imagen, hay un tope de **50 megapíxeles** por archivo: unos pocos bytes de header pueden declarar un lienzo enorme y agotar la memoria del server. Y un `jpg`/`png` que se detecta como imagen pero no se puede decodificar (cortado, corrupto) se rechaza con `400`.

El directorio además tiene **cuota total** (`UPLOAD_MAX_TOTAL_MB`, default 1 GB, ver [configuración](../configuracion.md)): si la suma de lo ya subido más el archivo nuevo la supera, responde `507` sin tocar el disco. Los archivos que ninguna fila de la base referencia (subidas abandonadas o fotos de autos ya borrados) se limpian solos pasados **7 días**.

**Errores:**

| Código | Mensaje | Cuándo |
|--------|---------|--------|
| `400` | `file is required` | Falta el campo `file` |
| `400` | `invalid multipart body` | Body no es multipart válido |
| `400` | `invalid image data` | `jpg`/`png` que no se puede decodificar |
| `413` | `request body too large` | Más de 5 MB |
| `413` | `image dimensions too large` | La imagen declara más de 50 megapíxeles |
| `415` | `only jpg, png or webp images are allowed` | No es imagen soportada |
| `507` | `storage quota exceeded` | El directorio estaría sobre la cuota |
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
