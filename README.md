# PayPhone Proxy

[![CI](https://github.com/tuxevil/payphone-proxy/actions/workflows/ci.yml/badge.svg)](https://github.com/tuxevil/payphone-proxy/actions/workflows/ci.yml)

PayPhone Proxy es un servicio HTTP open source que centraliza el flujo de pago
de varios proyectos que cobran para el mismo comercio de PayPhone. Cada
despliegue usa el token de una aplicación PayPhone Developer y cada proyecto se
autentica ante el proxy con una clave propia. La asignación de tiendas se hace
mediante alias de configuración, sin entregar credenciales de PayPhone a los
proyectos ni al navegador.

## Apoya el proyecto

Si este proyecto te resulta útil, puedes apoyar su mantenimiento mediante
[este enlace de pago](https://ppls.me/UbNkoknv8Ij9nGfgRDOg).

El servicio está pensado para separar ambientes: por ejemplo, un despliegue de
producción y otro de desarrollo, cada uno con su aplicación, token, base de
datos y dominio registrados en PayPhone. El mismo `StoreID` solo debe reutilizarse
entre aplicaciones si PayPhone lo autoriza explícitamente.

## Qué incluye

La primera versión implementa el producto PayPhone **Button por redirección**:

1. El backend de un proyecto crea un pago con `POST /v1/payments` y una clave
   de idempotencia.
2. El proxy reserva el pago, selecciona el proyecto y su alias de tienda,
   llama a `button/Prepare` y devuelve un `checkout_url` del propio proxy.
3. `/checkout/{token}` sirve una página mínima en el dominio WEB registrado y
   navega automáticamente al enlace `Anonymous` (`payWithCard`) de PayPhone.
   La respuesta incluye `Referrer-Policy: origin`.
4. PayPhone retorna a la ruta configurada. El proxy llama a
   `button/V2/Confirm`, valida los identificadores, el importe y la moneda, y
   persiste el resultado.
5. Si el proyecto configuró `return_url`, el navegador vuelve allí con el
   estado resumido. El proyecto debe consultar el API autenticado antes de
   entregar un producto o servicio.

Los estados locales son `preparing`, `pending`, `paid`, `cancelled`, `failed` y
`unknown`. Un retorno del navegador o un parámetro de query no constituye por sí
solo una confirmación financiera.

## Arquitectura y límites de confianza

```text
Backend del proyecto --(Bearer: clave del proyecto)--> PayPhone Proxy
                                                            |
                                                            +--> SQLite
                                                            +--> PayPhone API
                                                            +--> checkout central
                                                            +--> return_url del proyecto
```

- El token PayPhone vive únicamente en el proceso del proxy y se carga desde
  el entorno.
- Las claves `api_key` de `PAYMENT_PROJECTS_JSON` son credenciales internas del
  proxy; deben ser diferentes por proyecto y ambiente.
- El checkout no contiene tokens PayPhone, datos de tarjeta ni credenciales del
  proyecto.
- El callback valida el pago contra PayPhone antes de actualizar el estado.
- La base SQLite contiene identificadores de órdenes y datos operativos del
  pago; protégela como información sensible y respáldala con control de acceso.

El proxy no procesa ni almacena números de tarjeta, no reemplaza la
configuración de PayPhone Developer y no convierte automáticamente un token WEB
en una credencial para API Links o API Sale. Esas capacidades requieren
permisos y adaptadores verificados por separado.

## Requisitos

- Go 1.25.13 o posterior para desarrollo y compilación.
- SQLite local (el binario usa `modernc.org/sqlite`, sin CGO).
- Un reverse proxy que termine TLS en producción y reenvíe las rutas del
  servicio.
- Una aplicación PayPhone Developer y las tiendas autorizadas para ese token.

## Inicio local

La plantilla no contiene valores reales. Copia el archivo, reemplaza todos los
placeholders con secretos administrados fuera de Git y limita sus permisos:

```bash
cp .env.example .env
chmod 600 .env
# Edita .env con los valores de tu ambiente.
set -a
. ./.env
set +a
go run ./cmd/payphone-proxy
```

Verifica el proceso sin credenciales:

```bash
curl --fail http://localhost:8080/healthz
# {"status":"ok"}
```

Para Docker, monta `/data` como volumen persistente:

```bash
docker build -t payphone-proxy .
docker run --rm --name payphone-proxy \
  -p 127.0.0.1:8080:8080 \
  --env-file .env \
  -v payphone-proxy-data:/data \
  payphone-proxy
```

El ejemplo limita el puerto al loopback; en producción termina TLS en un
reverse proxy y conecta el contenedor a una red privada. En Docker usa
`DATABASE_PATH=/data/payments.db`. El contenedor se ejecuta como usuario sin
privilegios.

## Configuración

Las variables, reglas de validación y ejemplos anonimizados están en
[`docs/configuration.md`](docs/configuration.md). Como mínimo se necesitan:

- `PAYPHONE_TOKEN`: token secreto de la aplicación PayPhone de este ambiente.
- `PAYPHONE_STORES_JSON`: alias internos y sus `StoreID` autorizados.
- `PAYMENT_PROJECTS_JSON`: proyectos, claves internas, tienda y retorno.
- `PUBLIC_BASE_URL`: origen HTTPS registrado en PayPhone Developer para abrir
  el checkout.

`PAYPHONE_RESPONSE_URL` permite que el callback esté en otro host, siempre que
ese host también llegue a este proxy y publique exactamente su ruta. Si no se
define, se deriva de `PUBLIC_BASE_URL` y `PAYPHONE_RETURN_PATH`.

Nunca pongas en el repositorio un token PayPhone, una clave real de proyecto,
un `StoreID` real, un archivo `.env`, una base SQLite ni respuestas de API sin
anonimizar. Usa el gestor de secretos del orquestador en producción.

## API rápida

Crear un pago (el importe está en centavos enteros):

```bash
curl -X POST https://proxy.example/v1/payments \
  -H 'Authorization: Bearer <clave-del-proyecto>' \
  -H 'Idempotency-Key: pedido-ejemplo-1' \
  -H 'Content-Type: application/json' \
  -d '{"order_id":"pedido-ejemplo-1","amount":100,"currency":"USD","reference":"Demo"}'
```

Respuesta abreviada:

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

Consultar un pago:

```bash
curl https://proxy.example/v1/payments/pay_<id-opaco> \
  -H 'Authorization: Bearer <clave-del-proyecto>'
```

La misma `Idempotency-Key` con los mismos datos devuelve el pago original. Si
se reutiliza con datos diferentes, la respuesta es `409 Conflict`. Un proyecto
no puede consultar pagos de otro proyecto, aunque conozca su identificador.

El contrato completo de rutas, campos, errores y callback está en
[`docs/api.md`](docs/api.md).

## Despliegue detrás de un reverse proxy

El proceso escucha HTTP en `HTTP_ADDR`; no termina TLS. Configura el proxy
inverso para:

1. publicar `PUBLIC_BASE_URL/checkout/{token}` con HTTPS;
2. enrutar `PAYPHONE_RESPONSE_URL` (o la ruta derivada) al mismo proceso;
3. publicar `/healthz` para el chequeo de disponibilidad;
4. conservar el volumen que contiene `DATABASE_PATH`;
5. no registrar cabeceras `Authorization`, variables de entorno ni cuerpos con
   datos sensibles.

La guía de operación, copias de seguridad, rotación y diagnóstico está en
[`docs/operations.md`](docs/operations.md). Un error “No autorizado” al entrar
directamente al enlace de PayPhone suele indicar que faltó el `Referer` del
dominio WEB registrado; los compradores deben llegar mediante la página
`/checkout/{token}`.

## Desarrollo y calidad

```bash
gofmt -w ./cmd ./internal
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/payphone-proxy
```

La integración continua ejecuta estas comprobaciones en cada push y pull
request. Para contribuir, consulta [`CONTRIBUTING.md`](CONTRIBUTING.md) y las
reglas de divulgación en [`SECURITY.md`](SECURITY.md).

La investigación de las alternativas de PayPhone y sus límites está en
[`docs/payphone-official-research.md`](docs/payphone-official-research.md). La
propuesta de arquitectura y el informe de pruebas son documentos de contexto
histórico, con valores anonimizados.

## Estado del proyecto

El MVP cubre Button/Prepare/Confirm, idempotencia, aislamiento por proyecto,
checkout central, callback configurable, repositorios en memoria y SQLite,
configuración por entorno, health check y una imagen Docker reproducible.

La conciliación de retornos perdidos, outbox/webhooks firmados, límites por
proyecto, observabilidad avanzada, rotación automatizada y adaptadores para
Links/Sale requieren diseño y pruebas adicionales. Se gestionan en el tracker
de Beads; no uses este README como lista de tareas.

## Licencia

Este proyecto se distribuye bajo la [licencia MIT](LICENSE). Consulta el
archivo [`LICENSE`](LICENSE) para conocer el texto completo, los permisos y las
condiciones de uso. Para contribuir, consulta también
[`CONTRIBUTING.md`](CONTRIBUTING.md).

## Referencias oficiales

- [PayPhone Developer: configuración y credenciales](https://docs.payphone.app/configuracion-de-ambiente-y-credenciales)
- [PayPhone: botón de pago](https://docs.payphone.app/boton-de-pago)
- [PayPhone: API Link](https://docs.payphone.app/api-link)
- [PayPhone: API Sale](https://docs.payphone.app/api-sale)
- [PayPhone: códigos de error](https://docs.payphone.app/codigos-de-error)
