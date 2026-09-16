# Configuración

Este documento describe las variables que consume el binario y el formato de
los mapas JSON. La configuración se carga al iniciar el proceso; si falta una
variable obligatoria o el JSON es inválido, el proceso termina sin arrancar.

## Principios

- Un despliegue representa un ambiente y una aplicación PayPhone Developer.
  Mantén separados el token, el archivo de datos, el dominio y los proyectos de
  desarrollo y producción.
- `PAYPHONE_TOKEN` es el único token enviado a PayPhone. Nunca lo recibas en
  una petición HTTP ni lo incluyas en URLs, HTML o logs.
- Las `api_key` de los proyectos son credenciales del proxy, no tokens de
  PayPhone. Genera una clave aleatoria por proyecto y ambiente.
- Los alias de tienda son nombres internos. El valor asociado es el
  `StoreID` autorizado por PayPhone para el token de ese despliegue.
- Todos los valores reales deben vivir en el gestor de secretos del
  orquestador o en un `.env` local ignorado por Git. La plantilla
  [`.env.example`](../.env.example) solo contiene placeholders.

## Variables

| Variable | Obligatoria | Valor por defecto | Descripción |
| --- | --- | --- | --- |
| `HTTP_ADDR` | No | `:8080` | Dirección y puerto HTTP interno. TLS debe terminar en el reverse proxy. |
| `DATABASE_PATH` | No | `./data/payments.db` | Archivo SQLite. En Docker se recomienda `/data/payments.db` sobre un volumen persistente. |
| `PUBLIC_BASE_URL` | Sí | — | Origen absoluto desde el que se abre `/checkout/{token}`. Debe coincidir con el dominio WEB registrado en PayPhone. |
| `PAYPHONE_WEB_DOMAIN` | No | — | Alias legacy de `PUBLIC_BASE_URL`; solo se usa cuando la variable canónica está vacía. |
| `PAYPHONE_RETURN_PATH` | No | `/payphone/return` | Ruta de callback cuando no se define una URL completa. No admite query, fragmento, raíz ni rutas reservadas del API. |
| `PAYPHONE_RESPONSE_URL` | No | — | URL absoluta registrada en PayPhone para el retorno. Debe incluir una ruta no reservada distinta de `/`; puede usar otro host que llegue al mismo proxy. |
| `PAYPHONE_BASE_URL` | No | `https://pay.payphonetodoesposible.com/api` | Base del API oficial. Solo cambia este valor para un endpoint compatible controlado. |
| `PAYPHONE_TOKEN` | Sí | — | Bearer token secreto de la aplicación PayPhone de este ambiente; se rechazan placeholders de la plantilla. |
| `PAYPHONE_STORES_JSON` | Sí | — | Objeto JSON `alias -> StoreID`. Debe contener al menos una tienda. |
| `PAYMENT_PROJECTS_JSON` | Sí | — | Objeto JSON `project_id -> configuración del proyecto`. Debe contener al menos un proyecto. |

Las URL deben ser absolutas, no incluir usuario, contraseña, query ni fragmento.
Se exige HTTPS; HTTP solo se permite para `localhost`, `127.0.0.1` o
`::1`, lo que facilita el desarrollo local. `PAYPHONE_BASE_URL` también se
valida con estas reglas.

## Mapa de tiendas

El valor es un objeto JSON cuyas claves son alias internos y cuyos valores son
los identificadores entregados por PayPhone:

```dotenv
PAYPHONE_STORES_JSON='{"production":"replace-with-production-store-id","development":"replace-with-development-store-id"}'
```

Los alias no se envían al proveedor. El proxy los resuelve al crear el pago.
Puedes tener varios alias que apunten al mismo `StoreID` cuando esa sea una
decisión administrativa válida, pero cada aplicación PayPhone debe autorizar
explícitamente las tiendas que utiliza. No supongas que el proveedor permite
una cantidad ilimitada de tiendas.

## Mapa de proyectos

Cada proyecto tiene una clave propia del proxy, un alias de tienda existente y
un retorno opcional:

```dotenv
PAYMENT_PROJECTS_JSON='{"proyecto-produccion":{"api_key":"replace-with-project-key","store":"production","return_url":"https://project.example/payments/result"},"proyecto-desarrollo":{"api_key":"replace-with-another-project-key","store":"development","return_url":"https://dev-project.example/payment-result"}}'
```

Campos:

- `api_key`: obligatorio, único entre todos los proyectos del despliegue y de
  al menos 32 caracteres. Genera una clave aleatoria de alta entropía; no uses
  nombres, placeholders ni claves cortas (los placeholders conocidos se
  rechazan al iniciar). Es la credencial usada en
  `Authorization: Bearer ...`.
- `store`: obligatorio; debe coincidir con una clave de
  `PAYPHONE_STORES_JSON`.
- `return_url`: opcional. Si está vacío, el callback devuelve el estado en
  JSON al navegador. Si se define, debe ser HTTPS y el proxy solo añade
  `payment_id`, `order_id` y `status` a su query existente; no se acepta un
  destino enviado por el comprador.

Los identificadores de proyecto se devuelven en la API autenticada, por lo que
no incluyas nombres de clientes, correos ni datos personales en ellos.

## Separación de ambientes

Usa una configuración y una base distintas por ambiente:

| Elemento | Desarrollo | Producción |
| --- | --- | --- |
| Aplicación PayPhone | Aplicación y token de pruebas | Aplicación y token de producción |
| Origen WEB | Dominio de pruebas registrado | Dominio de producción registrado |
| Datos | Archivo/volumen SQLite independiente | Archivo/volumen SQLite independiente |
| Claves de proyecto | Generadas solo para desarrollo | Generadas solo para producción |
| Tiendas | Alias de tiendas autorizadas para pruebas | Alias de tiendas autorizadas para producción |

No permitas que un despliegue de desarrollo conozca el token o las claves de
producción. La selección del ambiente ocurre en el despliegue, no en una
petición del proyecto.

## Carga local segura

```bash
umask 077
cp .env.example .env
chmod 600 .env
# Completa .env en un editor local y no lo agregues a Git.
set -a
. ./.env
set +a
go run ./cmd/payphone-proxy
```

Antes de subir cambios comprueba que los archivos locales están ignorados:

```bash
git check-ignore -v .env .env.old data/payments.db
git status --short
```

No pegues la salida de esos archivos en issues, pull requests, Beads o
capturas. Si una credencial se expone, revócala y genera una nueva antes de
continuar.

## Reverse proxy y callback

El servicio solo escucha HTTP interno. El reverse proxy debe:

- terminar TLS para `PUBLIC_BASE_URL` y para `PAYPHONE_RESPONSE_URL` si usan
  hosts diferentes;
- enviar ambas rutas al mismo proceso;
- conservar el path exacto configurado (por ejemplo,
  `/payphone/response`);
- aplicar límites de tamaño y tiempo razonables sin quitar
  `Referrer-Policy: origin` de la respuesta de checkout;
- evitar logs de `Authorization`, cookies, variables de entorno y cuerpos
  completos.

La URL efectiva que PayPhone recibe es `PAYPHONE_RESPONSE_URL` cuando existe.
De lo contrario es `PUBLIC_BASE_URL` + `PAYPHONE_RETURN_PATH`.

## Cambios y rotación

Trata cualquier cambio de token, `StoreID` o clave de proyecto como un cambio
operativo:

1. prepara el nuevo valor en el gestor de secretos;
2. valida la configuración en una instancia aislada;
3. despliega y verifica `/healthz` y un pago controlado;
4. revoca el valor anterior cuando no existan pagos pendientes que dependan de
   él;
5. registra solo el resultado de la operación, nunca el valor secreto.

La versión actual guarda en cada pago el contexto de proyecto y tienda, pero no
implementa todavía un almacén de versiones de credenciales ni una conciliación
automática para pagos cuyo retorno se pierde.
