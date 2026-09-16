# PayPhone: investigación de documentación oficial

Fecha de consulta: 2026-09-15. Alcance: documentación pública de `docs.payphone.app`, con inventario obtenido de su sitemap. No se realizaron operaciones financieras ni llamadas autenticadas. Los hechos documentados se separan de las inferencias y de las pruebas pendientes.

## 1. Aplicaciones, credenciales y ambientes

La guía distingue aplicaciones **WEB** para botón, cajita y plugins, y **API** para Links/Sale. WEB registra un dominio y URL de respuesta; bloquea acceso desde otros dominios. El desarrollador debe tener al menos una tienda activa asociada. La aplicación proporciona Token y StoreID.

El ambiente se selecciona en Developer. En pruebas, las transacciones se aprueban sin conectar al procesador bancario; para simular pagos con PayPhone Personal se pueden invitar probadores. Producción conecta al sistema bancario. La guía no proporciona una URL base sandbox distinta. [Configuración y credenciales](https://docs.payphone.app/configuracion-de-ambiente-y-credenciales).

**No verificado:** cantidad máxima de aplicaciones o tiendas, autorización de un token para varias tiendas, posibilidad de usar un token WEB en endpoints API y viceversa. El límite de tres aplicaciones procede del usuario, no de una regla pública localizada. Tener `storeId` en una petición no demuestra qué tiendas están autorizadas por ese token.

## 2. Botón por redirección

- Aplicación WEB. `POST https://pay.payphonetodoesposible.com/api/button/Prepare` devuelve `paymentId`, `payWithCard` y `payWithPayPhone`.
- Petición: montos, `clientTransactionId`, `storeId`, `responseUrl`; opcional `cancellationUrl`.
- Formularios: vigencia de diez minutos; deben abrirse en navegador, sin iframe. La navegación debe originarse en el dominio registrado: la guía advierte que abrir el enlace directamente también puede bloquearse. Recomienda `Referrer-Policy: origin` u `origin-when-cross-origin`.
- El retorno contiene `id` y `clientTransactionId`. Confirmación: `POST https://pay.payphonetodoesposible.com/api/button/V2/Confirm`, cuerpo `{ "id": 123, "clientTxId": "identificador" }`, mismo Bearer token.
- Estado documentado: `3` aprobado, `2` cancelado; devuelve importe, moneda e identificadores.

La guía incluye `responseUrl` por petición y también describe la URL del panel; no precisa todas las restricciones entre ambas. [Botón de pago](https://docs.payphone.app/boton-de-pago).

**Inferencia de diseño:** el proxy debe servir una página real en el dominio registrado y navegar desde ella hacia PayPhone. Una cadena de redirecciones HTTP desde otro proyecto no demuestra que el navegador enviará el origen esperado. Hay que probarla; una página intermedia evita depender de esa suposición.

## 3. Cajita

La cajita usa aplicación WEB, requiere el dominio registrado y permite localhost durante desarrollo. Su SDK carga desde `cdn.payphonetodoesposible.com/box/v2.0/`; `PPaymentButtonBox` recibe el token en JavaScript. Incluye `storeId`, montos, moneda y `clientTransactionId` (máximo 50 caracteres). Los importes son enteros en centavos y el total suma base gravada, base no gravada, impuesto, servicio y propina.

El formulario dura diez minutos. Después del pago redirige a la URL configurada, con los dos identificadores. Su endpoint actual de confirmación es **`POST https://paymentbox.payphonetodoesposible.com/api/confirm`**, cuerpo `id`/`clientTxId`, mismo token. No debe confundirse con el endpoint del botón por redirección. [Cajita de pagos](https://docs.payphone.app/cajita-de-pagos).

**Inferencia de diseño:** alojarla en el dominio central es viable como alternativa visual, pero expone el token del SDK al navegador por diseño. Antes de elegirla con un token compartido hay que evaluar su alcance real y permisos; no asumir que es un token público de capacidades limitadas.

## 4. API Links

Aplicación API. `POST https://pay.payphonetodoesposible.com/api/Links`, Bearer token, devuelve un string con el enlace. Admite `storeId`, `oneTime`, `expireIn` (horas), `additionalData` y monto editable con permiso especial. `clientTransactionId`: máximo **15 caracteres**; referencia 100 y datos adicionales 250.

Se pueden compartir enlaces por mensajería, correo o QR. La guía establece **30 POST/minuto** y distingue caducidad del enlace de los diez minutos del formulario abierto. No permite iframe. Expresa que, después de pagar, el cliente recibe un comprobante pero **no hay retorno de respuesta a un sistema**. [API Link](https://docs.payphone.app/api-link).

**Inferencia de diseño:** adecuado para cobros asincrónicos, menos cómodo como checkout principal si se necesita volver al proyecto y actualizarlo automáticamente. El proxy puede emitir su propio enlace persistente y crear una sesión de botón al abrirlo, pero eso sería un enlace del proxy, no API Links. No se debe atribuir a Links la semántica de Prepare/Confirm.

## 5. API Sale y consulta de estado

Sale cobra **dentro de PayPhone Personal**: el cliente debe usar la app. `POST https://pay.payphonetodoesposible.com/api/Sale` recibe teléfono, código de país, montos e identificadores; admite `storeId`.

Consultas con Bearer token:

- `GET https://pay.payphonetodoesposible.com/api/Sale/{transactionId}`.
- `GET https://pay.payphonetodoesposible.com/api/Sale/client/{clientTransactionId}`.

La guía limita el GET de estado a **30 llamadas/minuto**. No aclara si la cuota se aplica por token, cuenta, IP o tienda. Incluye `responseUrl` opcional y dice que el servidor envía `id` y `clientTransactionID`, sin especificar contrato de entrega completo. [API Sale](https://docs.payphone.app/api-sale).

**Pendiente:** demostrar que las consultas Sale sirven también para botón, cajita y Links, especialmente antes de Confirm y cuando el comprador cierra el navegador. La ubicación del endpoint y ejemplos de Sale no bastan para afirmar cobertura universal.

**Inferencia:** si esa compatibilidad se valida, una tarea de recuperación puede consultar por ID comercial, obtener ID PayPhone y confirmar el pago. Debe respetar la cuota compartida y el plazo de confirmación; no prometer recuperación garantizada antes de probarlo.

## 6. Notificación externa

PayPhone documenta POST HTTPS a un método `NotificacionPago`, para pagos **aprobados** de botón, cajita, Links, tokenización y Sale. Requiere aprobación previa: el formulario solicita tienda y URL, y PayPhone puede denegarla.

Payload incluye `StoreId`, `ClientTransactionId`, `TransactionId`, `Amount`, `Currency`, `StatusCode`. ACK exitoso: `{ "Response": true, "ErrorCode": "000" }`; existe un código `333` para transacción duplicada.

La página no publica firma verificable, secreto compartido, política de reintentos, garantías de entrega ni orden respecto a Confirm. Tampoco declara que responder al webhook sustituya Confirm. [Notificación externa](https://docs.payphone.app/notificacion-externa).

**Inferencia:** no hacer depender el MVP de su aprobación. Si se habilita, tratarlo como entrada de conciliación, cotejar con PayPhone y aplicar procesamiento idempotente. El proxy sí puede generar sus propios webhooks firmados para cada proyecto, aunque PayPhone no habilite los suyos.

## 7. Confirmación, reversos y cancelación

Para botón/cajita, si no se confirma dentro de cinco minutos después del pago, PayPhone efectúa reverso automático. Reverse requiere el mismo token original y solo se solicita el mismo día hasta **20:00, hora de Ecuador**.

- `POST https://pay.payphonetodoesposible.com/api/Reverse` con `id`.
- `POST https://pay.payphonetodoesposible.com/api/Reverse/Client` con `clientId`.

No se documenta aquí reembolso parcial ni una API general de reembolsos posteriores al corte. [API Reverse](https://docs.payphone.app/api-reverse).

Cancel corresponde a una solicitud Sale todavía sin completar; requiere el mismo token y opera dentro de los primeros cinco minutos. Pasado ese plazo sin acción del cliente, se cancela automáticamente.

- `POST https://pay.payphonetodoesposible.com/api/Cancel` con `id`.
- `POST https://pay.payphonetodoesposible.com/api/Cancel/Client` con `clientId`.

[Cancelar solicitud](https://docs.payphone.app/cancelar-solicitud).

**Inferencia:** Confirm es una operación necesaria del ciclo de pago, no una mera lectura de estado. Persistir la versión de credenciales asociada a cada intento; no confiar en la configuración actual para operar transacciones creadas antes de una rotación. El cierre del navegador debe figurar en la matriz de pruebas.

## 8. Productos complementarios

### Comercios aliados

Token de terceros permite generar credenciales desde una aplicación partner, con token y StoreID propios para cada comercio aliado, asociado por RUC. Heredan la configuración técnica, pero los fondos se acreditan a la wallet del aliado. Permite listar tiendas y regenerar tokens. Requiere habilitación comercial previa. [Token de terceros](https://docs.payphone.app/token-de-terceros).

**Inferencia:** es la vía relevante si algún proyecto cobra para otra entidad. No equivale a varios proyectos propios organizados por tienda, y no se debe sustituir separación de beneficiarios con un `storeId` arbitrario.

### Split

Distribuye un cobro entre usuarios PayPhone, disponible en botón, cajita y Links. Permiso por tienda solicitado comercialmente. Tiene variantes inmediata y al día siguiente; la inmediata modifica las condiciones de recuperación de fondos y puede tomar saldo de la wallet del comercio al reversar. [Split](https://docs.payphone.app/split-de-pagos).

**Inferencia:** innecesario para distinguir proyectos; evaluar únicamente si se requiere repartir dinero.

### WebView y efectivo

WebView incrusta la web del comercio **dentro de la app PayPhone**, mediante el flujo de QR. Requiere autorización y funciona exclusivamente en producción. [WebView](https://docs.payphone.app/webview).

Código Efectivo requiere activación, agrega `transactionSummaryUrl` y consulta `GET /api/payment-button-box/payment-code/summary/{transactionId}/{client}` para obtener código y vencimiento. El pago posterior se comunica por notificación externa. [Código efectivo](https://docs.payphone.app/codigo-efectivo-en-boton-de-pago).

**Inferencia:** ninguno resuelve por sí mismo el límite de aplicaciones; efectivo necesita otro ciclo de estados, y WebView cambia el canal de compra.

### Pre-registro y consulta de usuarios

Pre-registro permite incorporar comercios; requiere aplicación API y autorización previa, con activación completada por el administrador del comercio. [Pre-registro](https://docs.payphone.app/preregistro-de-comercios).

`GET /api/Users/check/{phone}/region/{countryCode}` consulta si un teléfono está registrado en PayPhone Personal. [Consulta de usuario](https://docs.payphone.app/consultar-usuario).

### Plugins y tokenización

WooCommerce y PrestaShop usan aplicación WEB, token, StoreID y URL de respuesta del plugin. Ambas guías permiten omitir StoreID cuando hay una sola tienda; esto apoya la selección explícita de tienda, pero no demuestra autorización universal del token. WooCommerce permite reutilizar las credenciales al pasar a su plugin Cajita. [WooCommerce](https://docs.payphone.app/woocommerce), [PrestaShop](https://docs.payphone.app/prestashop).

La documentación de notificaciones menciona tokenización. No se encontró una guía técnica pública dedicada en el sitemap consultado; no se presupone soporte de suscripciones o cobros recurrentes habilitado.

## 9. Errores y calidad documental

El catálogo distingue tienda inexistente (`100`), terminal ajeno a tienda (`101`), límites de montos (`102`/`103`), token inválido (`802`), validación (`800`), transacción inexistente (`20`) y errores de servicio (`500`/`501`). Son códigos del cuerpo de PayPhone, no necesariamente códigos HTTP. [Códigos de error](https://docs.payphone.app/codigos-de-error).

Los ejemplos no deben convertirse directamente en implementación: algunos incluyen identificadores más largos que el máximo declarado para Links; ciertos tipos y campos obligatorios son inconsistentes entre tablas y ejemplos. Conservar payloads reales anonimizados durante la validación y usar el contrato probado por adaptador.

## 10. Recomendación y verificación necesaria

**Recomendación de ingeniería:** comenzar con botón por redirección, checkout y retorno centrales, token en servidor y credenciales propias del proxy por proyecto. Una aplicación WEB de desarrollo y otra WEB de producción encajan con esa primera fase. La compatibilidad con múltiples tiendas del mismo token sigue siendo una hipótesis verificable, no un resultado de esta investigación.

Para ampliar posteriormente a otros canales, separar adaptadores y capacidades. No prometer simultáneamente WEB + API en ambos ambientes dentro de dos aplicaciones: las guías distinguen sus tipos y no resuelven esa compatibilidad.

Pruebas que cierran las incertidumbres, primero en la aplicación de pruebas:

1. Mismo token, dos tiendas autorizadas: verificar creación, pago, consulta, Confirm y tienda receptora.
2. Dominio central y proyectos en dominios distintos: probar navegador real y política de referente.
3. Precedencia/restricción de `responseUrl` del panel frente al campo de Prepare.
4. Cerrar el navegador después de pagar: verificar consulta por ID comercial y Confirm dentro del plazo.
5. Repetición de Confirm, reintentos, timeouts y concurrencia sin doble efecto local.
6. Credencial WEB contra capacidades API únicamente si se desea ampliar el alcance; registrar qué está habilitado sin extrapolar.
7. Alcance real de cuotas, asociación de tiendas y rotación de tokens.

La arquitectura debe poder funcionar con un solo StoreID si la hipótesis multitienda falla: el enrutamiento del proyecto puede mantenerse en el registro interno de cada intento. Esa alternativa organiza los pagos dentro del proxy, aunque pierde la separación por tienda en PayPhone.
