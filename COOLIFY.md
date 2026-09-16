# Despliegue en producción con Coolify

El repositorio incluye una imagen Docker multi-etapa, un `docker-compose.yml`
orientado a Coolify y un volumen dedicado para la base SQLite. Coolify termina
TLS y enruta el dominio público hacia el puerto interno `8080`; el proceso no
debe exponerse directamente a Internet.

## Requisitos previos

- Una aplicación PayPhone Developer separada para cada ambiente.
- El dominio WEB registrado en PayPhone para `PUBLIC_BASE_URL`.
- La URL de respuesta completa registrada en PayPhone para
  `PAYPHONE_RESPONSE_URL` (si se usa un host distinto).
- DNS apuntando al servidor de Coolify y un certificado TLS gestionado por
  Coolify.
- Un único contenedor activo por base SQLite; no escales horizontalmente esta
  instancia.

No guardes tokens, claves, `StoreID`, archivos `.env` ni bases SQLite en Git.
Los valores secretos se crean como variables protegidas en Coolify.

## Opción recomendada: aplicación Dockerfile desde Git

1. En Coolify crea `New Resource` → `Application` y selecciona el repositorio
   Git.
2. Usa la rama `main`, directorio base `/` y `Dockerfile` `/Dockerfile`.
3. Configura `Ports Exposes` como `8080` y añade el dominio HTTPS del checkout.
   Si `PAYPHONE_RESPONSE_URL` usa otro host, añade también ese dominio al
   mismo recurso o enrútalo al mismo contenedor.
4. Añade las variables de entorno de la tabla siguiente. Marca como secretas
   todas las que contengan credenciales o identificadores operativos.
5. En `Persistent Storage` crea un volumen con destino `/data`. No montes solo
   `/data/payments.db`: SQLite también puede crear archivos auxiliares junto a
   la base.
6. Despliega una revisión fija de `main` y comprueba los logs y el endpoint
   `/healthz` antes de registrar el retorno en PayPhone.

Coolify puede usar el `HEALTHCHECK` incluido en la imagen. El probe es un
binario estático dentro de la imagen final, por lo que no requiere instalar
`curl` ni `wget` en el contenedor distroless.

## Variables de entorno

| Variable | Valor recomendado | Secreto |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | No |
| `DATABASE_PATH` | `/data/payments.db` | No |
| `PUBLIC_BASE_URL` | `https://web.example.com` | No |
| `PAYPHONE_RETURN_PATH` | `/payphone/return` | No |
| `PAYPHONE_RESPONSE_URL` | URL completa registrada, si aplica | No |
| `PAYPHONE_BASE_URL` | `https://pay.payphonetodoesposible.com/api` | No |
| `PAYPHONE_TOKEN` | Token de la aplicación Developer del ambiente | Sí |
| `PAYPHONE_STORES_JSON` | `{"production":"<store-id>"}` | Sí |
| `PAYMENT_PROJECTS_JSON` | `{"proyecto":{"api_key":"<clave-de-32+-caracteres>","store":"production"}}` | Sí |

Los dos mapas JSON deben pegarse como JSON válido en Coolify, sin comillas
externas de shell. Genera una clave distinta y aleatoria para cada proyecto y
ambiente. Si no defines `PAYPHONE_RESPONSE_URL`, el proxy deriva el callback a
partir de `PUBLIC_BASE_URL` y `PAYPHONE_RETURN_PATH`.

## Opción alternativa: Docker Compose desde Git

1. Crea `New Resource` → `Docker Compose` desde el repositorio y la rama
   `main`.
2. Usa el archivo `/docker-compose.yml` de la raíz.
3. Define las mismas variables de entorno en Coolify; las referencias `${VAR}`
   del Compose aparecen como variables configurables y las expresiones `:?`
   bloquean un despliegue incompleto.
4. Configura el dominio para el servicio `payphone-proxy` en el puerto interno
   `8080`. El Compose no publica un puerto del host: el proxy de Coolify debe
   ser la única entrada pública.
5. Conserva el volumen `payphone-proxy-data` y verifica el health check del
   servicio después de cada redeploy.

## Verificación y operación

Desde una red autorizada:

```bash
curl --fail https://web.example.com/healthz
```

Antes de aceptar pagos reales, verifica que:

- `/checkout/{token}` se abre desde el dominio WEB registrado;
- `PAYPHONE_RESPONSE_URL` llega al mismo contenedor y conserva su query;
- el volumen `/data` sobrevive a un redeploy;
- los logs no contienen `Authorization`, tokens, claves ni URLs de checkout;
- la copia de seguridad de SQLite y la restauración fueron probadas.

SQLite no soporta varias réplicas activas de este servicio. Para alta
disponibilidad o conciliación de estados `unknown` se necesita un diseño
adicional antes de escalar.

## Referencias oficiales de Coolify

- [Aplicaciones con Dockerfile](https://coolify.io/docs/applications/builds/dockerfile)
- [Aplicaciones Docker Compose](https://coolify.io/docs/applications/builds/docker-compose)
- [Variables de entorno](https://coolify.io/docs/applications/configuration/environment-variables)
- [Almacenamiento persistente](https://coolify.io/docs/applications/configuration/persistent-storage)
- [Health checks](https://coolify.io/docs/applications/configuration/health-checks)
