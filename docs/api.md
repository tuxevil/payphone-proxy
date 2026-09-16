# API HTTP

El API del proxy tiene tres superficies: API autenticada para los proyectos,
checkout público y callback de PayPhone. Las respuestas son JSON salvo el
checkout HTML y la redirección final.

Los ejemplos usan dominios, claves e identificadores ficticios. Sustitúyelos en
tu entorno y no publiques valores reales.

## Autenticación

Las rutas `/v1/payments` requieren:

```http
Authorization: Bearer <clave-del-proyecto>
```

La clave se compara en tiempo constante contra `PAYMENT_PROJECTS_JSON`. El
cliente nunca envía ni selecciona el token de PayPhone. Una clave inválida
responde `401 Unauthorized` sin revelar qué proyecto existe.

## Crear un pago

```http
POST /v1/payments
Content-Type: application/json
Authorization: Bearer <clave-del-proyecto>
Idempotency-Key: pedido-ejemplo-1

{
  "order_id": "pedido-ejemplo-1",
  "amount": 100,
  "currency": "USD",
  "reference": "Compra de demostración"
}
```

Campos:

| Campo | Tipo | Reglas |
| --- | --- | --- |
| `order_id` | string | Obligatorio, entre 1 y 200 caracteres. Es la referencia del proyecto. |
| `amount` | integer | Obligatorio, positivo y expresado en centavos. |
| `currency` | string | Obligatorio; la primera versión acepta únicamente `USD` (sin distinguir mayúsculas). |
| `reference` | string | Opcional, hasta 100 caracteres. |

`Idempotency-Key` es obligatorio, admite de 1 a 128 caracteres y se aplica
por proyecto. El proxy reserva el pago antes de llamar al proveedor. Si una
petición se repite con la misma clave y los mismos datos, devuelve el pago
original sin crear otra sesión. Si los datos difieren, devuelve
`409 Conflict`.

Respuesta nueva: `201 Created`. Reintento idempotente: `200 OK`.

```json
{
  "id": "pay_<id-opaco>",
  "project_id": "proyecto-ejemplo",
  "order_id": "pedido-ejemplo-1",
  "amount": 100,
  "currency": "USD",
  "status": "pending",
  "checkout_url": "https://registered.example/checkout/<token-opaco>",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z"
}
```

El `checkout_url` es una URL pública opaca del proxy. No la sustituyas por
`payWithCard` ni `payWithPayPhone` de PayPhone: la página intermedia es la
que conserva el origen WEB registrado.

## Consultar un pago

```http
GET /v1/payments/<payment-id>
Authorization: Bearer <clave-del-proyecto>
```

Devuelve `200 OK` con el mismo esquema de pago. La autorización se comprueba
por proyecto: un identificador perteneciente a otro proyecto se presenta como
`404 Not Found`. El endpoint no permite listar pagos ni buscar por
`order_id`.

El proyecto debe consultar este endpoint servidor a servidor antes de entregar
un bien o habilitar una funcionalidad. Los parámetros `status` de una URL de
navegador son solo una ayuda para la interfaz.

## Estados

| Estado | Significado |
| --- | --- |
| `preparing` | Reserva local creada; la preparación ante PayPhone aún está en curso. |
| `pending` | PayPhone devolvió una sesión y el comprador puede pagar. |
| `paid` | `Confirm` validó una respuesta aprobada. |
| `cancelled` | `Confirm` devolvió el código de cancelación documentado. |
| `failed` | Fallo definitivo de preparación, confirmación o respuesta no aprobada. |
| `unknown` | Error de red u otro resultado que no permite afirmar el estado. |

La versión actual no reconcilia automáticamente estados `unknown` ni
retornos perdidos.

## Checkout público

```http
GET /checkout/<public-token>
```

- No requiere `Authorization`.
- Solo funciona mientras el pago está `pending`.
- Devuelve `200 OK`, `Content-Type: text/html` y
  `Referrer-Policy: origin`.
- Navega automáticamente al enlace de tarjeta que PayPhone devolvió para esa
  sesión (ruta `Anonymous`).
- No muestra una opción separada de PayPhone wallet ni expone el token secreto
  del proveedor.

Un token inexistente responde `404 Not Found`. Un pago que ya no está
pendiente responde `409 Conflict`. El checkout no debe incrustarse en un
iframe ni abrirse pegando directamente el enlace final de PayPhone: la
navegación debe originarse en el dominio WEB registrado para conservar el
`Referer` esperado por el proveedor.

## Retorno de PayPhone

La ruta atendida es el path de `PAYPHONE_RESPONSE_URL` o, si esa variable está
vacía, `PAYPHONE_RETURN_PATH` bajo `PUBLIC_BASE_URL`.

PayPhone retorna un identificador numérico y el identificador de transacción
del cliente:

```http
GET /payphone/response?id=<id-payphone>&clientTransactionId=<client-tx-opaco>
```

El proxy también acepta las variantes de capitalización
`clientTransactionID` y `clientTxId`. Rechaza el retorno con:

- `400 invalid_return` si falta `id` o el identificador de cliente;
- `422 confirmation_mismatch` si la respuesta confirmada no coincide en
  transacción, importe o moneda;
- `502 provider_error` si PayPhone no responde correctamente;
- `404 not_found` si no existe la reserva local.

Para un pago pendiente, el proxy llama a
`POST /button/V2/Confirm` con el token del despliegue, valida la respuesta,
guarda el nuevo estado y evita aceptar un identificador de proveedor distinto
en una repetición.

### Destino del proyecto

Si el proyecto tiene `return_url`, el proxy responde `303 See Other` y añade
tres parámetros URL codificados:

```text
https://project.example/payments/result?payment_id=pay_<id-opaco>&order_id=pedido-ejemplo-1&status=paid
```

Si no hay `return_url`, responde `200 OK` con el objeto de pago en JSON. Nunca
se acepta un destino arbitrario desde la query del navegador. El consumidor
debe volver a llamar a `GET /v1/payments/<payment-id>` con su clave para tomar
una decisión financiera.

## Health check

```http
GET /healthz
```

Responde siempre `200 OK` con `{"status":"ok"}` y no requiere credenciales.
Publícalo para el balanceador, pero restringe las demás rutas administrativas
(no existe una consola pública en esta versión).

## Errores

Todas las rutas JSON de error usan:

```json
{
  "error": {
    "code": "invalid_request",
    "message": "descripción segura para el cliente"
  }
}
```

Códigos habituales:

| HTTP | `code` | Causa |
| --- | --- | --- |
| 400 | `invalid_request` | JSON inválido, campo desconocido, importe/moneda inválidos o falta de idempotencia. |
| 400 | `invalid_return` | Faltan parámetros del retorno. |
| 401 | `unauthorized` | Bearer ausente o incorrecto. |
| 404 | `not_found` | Pago, checkout o ruta inexistente. |
| 409 | `idempotency_conflict` | Misma clave con datos diferentes. |
| 409 | `checkout_unavailable` | El pago ya no está pendiente. |
| 422 | `confirmation_mismatch` | Confirmación de PayPhone no coincide con la reserva. |
| 502 | `provider_error` | PayPhone rechazó o no pudo procesar la llamada. |
| 500 | `internal_error` | Error interno no esperado. |

Los mensajes se mantienen deliberadamente genéricos: no incluyen el Bearer,
el token de PayPhone, StoreID, cuerpos completos del proveedor ni secretos de
configuración.

## Integración recomendada

1. Genera un `Idempotency-Key` estable por intento lógico de la orden.
2. Guarda `id` y abre `checkout_url` desde el navegador del comprador.
3. Trata la URL de retorno como señal de actualización de interfaz, no como
   autorización.
4. Consulta el pago desde el backend con la clave del proyecto.
5. Entrega el producto solo cuando el API autenticado reporte `paid`.
6. Maneja reintentos y estados no terminales sin crear otra orden por accidente.
