# Uploading files

## `POST /uploads`

Uploads an image and returns its local path. Requires login (any role). The returned path is later used as `photo_url` in `POST /seller/cars` or as `proof_url` in `POST /reservations/{id}/payment`.

**Body:** `multipart/form-data` with the `file` field.

**Answers** `201`:

```json
{"url":"/uploads/3f9a1c2e4b5d6f708192a3b4c5d6e7f8.jpg"}
```

Rules: only `jpg`, `png` or `webp` (detected by content, not by extension), max **5 MB** per file. The name is generated on the server, the original one is ignored.

`jpg` and `png` images are **re-encoded** on the server (jpeg quality 85, png lossless): the stored file is just pixels, so EXIF metadata —GPS, camera model, date— never reaches the disk or the public response. This matters: a car photo usually carries the coordinates of the seller's house. `webp` is stored as is, with its metadata (the Go stdlib ships no webp encoder).

Because re-encoding has to decode the image, there is a cap of **50 megapixels** per file: a few header bytes can declare an enormous canvas and exhaust the server memory. And a `jpg`/`png` detected as an image that cannot be decoded (truncated, corrupt) is rejected with `400`.

The directory also has a **total quota** (`UPLOAD_MAX_TOTAL_MB`, default 1 GB, see [configuration](../configuration.md)): if what is already uploaded plus the new file exceeds it, it answers `507` without touching the disk. Files that no database row references (abandoned uploads or photos of cars already deleted) are swept automatically after **7 days**.

**Errors:**

| Status | Message | When |
|--------|---------|------|
| `400` | `file is required` | The `file` field is missing |
| `400` | `invalid multipart body` | The body is not valid multipart |
| `400` | `invalid image data` | A `jpg`/`png` that cannot be decoded |
| `413` | `request body too large` | More than 5 MB |
| `413` | `image dimensions too large` | The image declares more than 50 megapixels |
| `415` | `only jpg, png or webp images are allowed` | Not a supported image |
| `507` | `storage quota exceeded` | The directory would go over the quota |
| `401` | see [00-general](00-general.md) | No token |

---

## `GET /uploads/{name}`

Returns the image. Requires no login. Answers `404 {"error":"upload not found"}` if it does not exist, is a directory, or the extension is not an image. With `Cache-Control: public, max-age=31536000, immutable`.

Flow example:

```sh
url=$(curl -s -F "file=@photo.jpg" -H "Authorization: Bearer $TOKEN" \
  "$BASE/uploads" | jq -r .url)
curl -s -X POST "$BASE/seller/cars" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"Toyota Yaris\",\"price_per_day\":45000,\"photo_url\":\"$url\"}"
```

The images live in the `UPLOAD_DIR` directory (default `uploads/`). See [configuration](../configuration.md).