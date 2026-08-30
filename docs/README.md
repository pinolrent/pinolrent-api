# Documentación de Pinol Rent API

La documentación de este proyecto está en español; los comentarios del código
están en inglés.

- **[Arquitectura](arquitectura.md)** — stack, layout del repositorio, modelo de datos, máquina de estados y reglas de dominio.
- **[Configuración](configuracion.md)** — variables de entorno, `.env`, arranque en producción.
- **[Referencia de la API](api/00-general.md)** — convenciones y cada endpoint:
  - [Auth](api/auth.md) · [Autos](api/cars.md) · [Reservas](api/reservations.md) · [Pagos](api/payments.md)
- **[Flujo completo](flujo-completo.md)** — un recorrido real con `curl`, del registro a la confirmación.
- **[Desarrollo](desarrollo.md)** — Makefile, herramientas, colección Bruno, tests y lint.

> El [README raíz](../README.md) es la entrada rápida: qué es el proyecto y cómo levantarlo en 30 segundos.