# Seguridad

La seguridad del proxy depende de que las credenciales permanezcan en el
ambiente de ejecución y de que cada proyecto reciba únicamente la autorización
que necesita. Esta política describe cómo reportar vulnerabilidades sin publicar
secretos ni datos de compradores.

## Versiones

La rama principal recibe correcciones mientras el proyecto se encuentra en
desarrollo activo. Las imágenes o commits desplegados deben identificarse por
una versión o digest, no por una etiqueta mutable.

## Reportar una vulnerabilidad

No abras un issue público para una vulnerabilidad, un token, una clave de
proyecto, un `StoreID`, una URL de checkout ni datos de un pago.

Usa [GitHub Security Advisories](https://github.com/tuxevil/payphone-proxy/security/advisories/new)
para enviar un reporte privado. Incluye:

- descripción y componente afectado;
- pasos mínimos para reproducirlo con valores ficticios;
- impacto y condiciones necesarias;
- versión o commit afectado;
- una propuesta de mitigación, si existe.

Redacta cualquier credencial, identificador real, dominio interno, dirección IP,
correo, número de orden o dato de tarjeta antes de adjuntar logs. Si el
formulario de advisories no estuviera disponible, solicita un canal privado a
los mantenedores mediante el perfil público del repositorio; no publiques los
detalles técnicos en un issue.

El equipo acusará recibo cuando pueda, validará el impacto y coordinará una
corrección y un aviso. No prometemos un plazo fijo mientras el proyecto no
tenga un equipo de seguridad dedicado.

## Reglas para contribuciones

- Nunca confirmes `.env`, bases SQLite, tokens, claves, certificados privados
  ni respuestas sin anonimizar.
- No pongas secretos en nombres de ramas, mensajes de commit, issues, Beads,
  capturas, fixtures ni documentación.
- Usa placeholders como `replace-with-example` y dominios reservados como
  `example` en las pruebas.
- Mantén el token PayPhone en el servidor; no lo envíes al navegador ni a los
  proyectos consumidores.
- No confíes en una query string del navegador para autorizar una orden:
  consulta el estado autenticado del proxy.
- Si detectas una exposición, revoca primero la credencial y luego reporta el
  incidente con el valor omitido.

La política no concede permiso para realizar pruebas contra cuentas o sistemas
de terceros. Prueba únicamente instalaciones y datos que tengas autorización
para usar.
