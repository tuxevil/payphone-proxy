# Operación y despliegue

Esta guía cubre una instalación detrás de un reverse proxy o un orquestador de
contenedores. Los nombres de host, identificadores y valores de secreto son
siempre específicos de cada instalación y no deben aparecer en tickets,
capturas, commits ni documentación pública.

## Topología recomendada

```text
Internet
   |
   | HTTPS (dominio WEB registrado)
   v
Reverse proxy / balanceador
   | HTTP privado
   v
PayPhone Proxy :8080 ----> PayPhone API oficial
   |
   +---- volumen persistente: /data/payments.db
```

Si `PAYPHONE_RESPONSE_URL` usa un host distinto de
`PUBLIC_BASE_URL`, ambos deben terminar en el mismo proceso. PayPhone debe
poder alcanzar la ruta completa del callback; no basta con publicar solo
`/checkout`.

## Despliegue con Docker

Construye la imagen desde una revisión fija del repositorio y publica solo el
puerto interno que necesite el reverse proxy:

```bash
docker build --pull -t payphone-proxy:<version> .
docker run -d --name payphone-proxy \
  --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  --env-file /ruta/segura/payphone-proxy.env \
  -v payphone-proxy-data:/data \
  payphone-proxy:<version>
```

La imagen usa una etapa de compilación reproducible, un binario estático y un
usuario sin privilegios. En producción define `DATABASE_PATH=/data/payments.db`
y no montes el archivo `.env` dentro de una ruta servida por HTTP.

En Coolify, Kubernetes u otro orquestador aplica el mismo modelo:

- guardar `PAYPHONE_TOKEN` y las claves de proyecto como secretos, no como
  variables visibles en una imagen ni en el repositorio;
- montar un volumen exclusivo para `DATABASE_PATH`;
- hacer que el contenedor escuche solo en la red privada;
- configurar un health check HTTP para `/healthz`;
- fijar la versión de la imagen o del commit que se desplegó;
- mantener separadas las aplicaciones de desarrollo y producción.

## TLS y enrutamiento

El proxy de aplicación no termina TLS. El reverse proxy debe:

1. emitir y renovar certificados para cada origen;
2. reenviar `GET /checkout/{token}`, `GET /healthz` y la ruta exacta de
   `PAYPHONE_RESPONSE_URL`;
3. conservar el método, la query y los encabezados necesarios del retorno;
4. establecer límites de tamaño y tiempo para peticiones;
5. evitar que el callback se redirija a otra aplicación por una regla genérica;
6. conservar `Referrer-Policy: origin` en la respuesta de checkout.

No expongas una página de administración: esta versión no tiene consola.
Restringe el acceso de `/v1/payments` mediante autenticación de proyecto y,
si es posible, reglas de red que permitan únicamente los backends de los
proyectos.

## Persistencia y copias

SQLite es apropiado para una primera instancia con un único proceso y volumen
persistente. No almacenes la base en el filesystem efímero del contenedor.

Una copia en caliente puede hacerse con la herramienta de SQLite o el mecanismo
de snapshots del volumen. Ejemplo genérico:

```bash
install -d -m 700 /ruta/privada/backups
sqlite3 /data/payments.db ".backup '/ruta/privada/backups/payments-<fecha>.db'"
chmod 600 /ruta/privada/backups/payments-<fecha>.db
```

Protege las copias con el mismo nivel de acceso que la base original. Prueba la
restauración periódicamente en un entorno aislado y verifica que los pagos
conservan su idempotencia. Define una política de retención compatible con tus
obligaciones legales; el proxy no decide cuánto tiempo deben conservarse los
datos de cada proyecto.

La implementación actual no tiene replicación, migraciones versionadas ni
conciliación automática de estados `unknown`. Para alta disponibilidad,
varios procesos o recuperación de retornos perdidos, registra trabajo de diseño
antes de escalar.

## Health checks y observabilidad

El chequeo mínimo es:

```bash
curl --fail https://proxy.example/healthz
```

Debe comprobarse además, desde una red autorizada, que:

- el callback devuelve una respuesta controlada cuando faltan parámetros;
- `/checkout/{token}` conserva la política de referrer;
- la base sigue disponible después de reiniciar el proceso;
- el reverse proxy no altera la query de PayPhone.

Registra métricas agregadas por ambiente y proyecto (cantidad y latencia de
`Prepare`, `Confirm`, estados finales y errores HTTP), pero no valores de
tokens, claves, `StoreID`, números de tarjeta, cuerpos completos del proveedor
ni URLs con tokens públicos. Redacta `Authorization` antes de que cualquier
proxy o sistema de logs la persista. La versión actual emite únicamente logs
de arranque y errores de ciclo de vida; la observabilidad detallada queda como
trabajo futuro.

## Rotación de credenciales

### Claves internas de proyectos

1. Genera una nueva clave aleatoria fuera del repositorio.
2. Coordina el cambio con el backend consumidor.
3. Actualiza el secreto del ambiente y reinicia de forma controlada.
4. Verifica creación y consulta con la clave nueva.
5. Revoca la anterior cuando no queden clientes que la usen.
6. Registra el momento y el resultado, no la clave.

La versión actual acepta una sola clave por proyecto. Una rotación sin
superposición puede requerir una ventana breve de coordinación; no pongas dos
claves en el mismo campo ni las guardes en el código.

### Token de PayPhone y tiendas

El token y los `StoreID` están asociados a la aplicación de PayPhone y al
ambiente. Valida el nuevo conjunto con una prueba controlada, conserva la base
de datos y evita revocar el valor anterior mientras existan sesiones que deban
confirmarse. El servicio todavía no guarda versiones históricas de credenciales
por intento, por lo que una rotación debe planificarse considerando la ventana
de confirmación del proveedor.

Si una credencial aparece en un commit, log o ticket:

- revócala inmediatamente en PayPhone o en el gestor correspondiente;
- genera un reemplazo;
- elimina la copia del sistema de logs y del artefacto publicado cuando sea
  posible;
- reporta solo que hubo una exposición, sin volver a pegar el valor.

## Diagnóstico

### `No autorizado` en el formulario de PayPhone

PayPhone puede validar el `Referer` del dominio WEB registrado. Abrir el enlace
final de PayPhone desde una mensajería o la barra de direcciones no reproduce el
flujo esperado. Haz que el comprador abra `checkout_url` del proxy desde una
página de tu origen registrado. Comprueba también que:

- el certificado TLS y el host coinciden exactamente con Developer;
- `PUBLIC_BASE_URL` no tiene una barra o path diferente al registrado;
- el checkout devuelve `Referrer-Policy: origin`;
- no hay un redirect del reverse proxy que cambie el origen antes de navegar.

No compartas el enlace final de PayPhone en incidencias: contiene un
identificador efímero.

### `invalid_return: PayPhone id is required`

La ruta de callback necesita el parámetro numérico `id` y
`clientTransactionId` (también acepta variantes documentadas). Revisa que el
reverse proxy conserve la query y que PayPhone esté usando la URL registrada.
Nunca fabriques esos valores desde el navegador.

### `503` o `404` al volver

Confirma que `PAYPHONE_RESPONSE_URL` apunta al proceso correcto, que su path
coincide con `PayPhoneReturnPath()` y que el certificado es válido. Prueba
`GET /healthz` en el mismo origen y revisa la regla de enrutamiento antes de
reintentar un pago.

### Estado desconocido

Un timeout no demuestra que PayPhone no recibió la petición. Conserva la reserva
local, no crees otro cobro automáticamente y sigue el procedimiento de
conciliación del proyecto. El MVP no incluye un worker de reconciliación; esa
limitación debe quedar visible para soporte.

## Respuesta a incidentes

Ante un acceso no autorizado o una fuga de datos:

1. aísla el ambiente afectado y conserva los logs mínimos necesarios;
2. revoca y rota credenciales potencialmente expuestas;
3. bloquea temporalmente el endpoint o el proyecto comprometido;
4. determina si hubo pagos duplicados, cambios de estado o acceso cruzado;
5. informa a los responsables de cada proyecto y a PayPhone cuando corresponda;
6. documenta el incidente con identificadores anonimizados;
7. publica una corrección sin incluir el material sensible.

El canal de divulgación técnica está descrito en [`SECURITY.md`](../SECURITY.md).
