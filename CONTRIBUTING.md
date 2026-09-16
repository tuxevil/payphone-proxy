# Contribuir

Gracias por colaborar con PayPhone Proxy. El proyecto busca una base pequeña,
auditable y segura para integrar varios proyectos con un mismo comercio de
PayPhone.

## Antes de empezar

1. Lee el [README](README.md), la [configuración](docs/configuration.md) y la
   [política de seguridad](SECURITY.md).
2. Comprueba que Go 1.24 o posterior esté instalado.
3. Trabaja con datos ficticios y un ambiente PayPhone autorizado.
4. No copies secretos, bases SQLite ni identificadores operativos al árbol de
   trabajo.

El proyecto se distribuye bajo la [licencia MIT](LICENSE). Consulta el archivo
`LICENSE` para conocer los permisos y condiciones antes de redistribuir una
copia o incorporar el código a un producto.

## Flujo de trabajo

- Abre primero un issue público para cambios funcionales, salvo que sea un
  reporte de seguridad (usa el canal privado de [SECURITY.md](SECURITY.md)).
- Mantén cada pull request enfocado y explica el comportamiento observable.
- Los mantenedores usan Beads para el seguimiento durable de tareas. Los
  colaboradores externos no necesitan acceso a la base local de Beads; enlaza
  el issue o pull request público correspondiente.
- No pegues en issues, Beads o commits valores de `.env`, tokens, claves,
  `StoreID`, URLs de checkout, dominios internos, direcciones IP o datos
  personales. Usa placeholders y datos anonimizados.

## Cambios de código

Respeta la separación existente:

- `cmd/payphone-proxy` conecta configuración, repositorio, cliente y HTTP.
- `internal/config` valida variables y mapas JSON.
- `internal/payments` contiene estados, idempotencia, aislamiento y
  confirmación.
- `internal/payphone` es la frontera con el proveedor y debe ser sustituible
  por un fake en tests.
- `internal/httpapi` define el contrato HTTP, checkout y callback.

Usa la biblioteca estándar cuando sea suficiente, importes enteros en
centavos, validación explícita y errores seguros. No aceptes un token PayPhone
desde una petición. Los cambios de comportamiento deben incluir tests en la
frontera HTTP o del cliente del proveedor.

## Calidad local

Ejecuta antes de abrir el pull request:

```bash
gofmt -w ./cmd ./internal
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/payphone-proxy
```

Si modificas Docker o workflows, valida también la construcción de la imagen
y revisa que no copie `.env`, `data/` ni otros artefactos locales.

## Pull requests

La descripción debe contener:

- qué problema resuelve y qué queda fuera de alcance;
- decisiones de configuración o migración;
- pruebas ejecutadas y su resultado;
- impacto operativo y estrategia de reversión;
- confirmación de que no se incluyeron secretos ni datos privados.

Los ejemplos de documentación deben usar dominios reservados, identificadores
opacos y valores que no puedan autenticarse contra PayPhone. No publiques
capturas del panel Developer ni enlaces de pago reales.

## Cambios de documentación

La documentación pública debe estar en español claro, distinguir hechos de
inferencias y enlazar la fuente oficial cuando describa PayPhone. Si un
documento de prueba contiene datos históricos, anonímizalos y márcalo como
informe de contexto. Las tareas futuras se registran en Beads, no en listas
TODO que puedan quedar desactualizadas en el README.

## Commits

Usa mensajes breves y descriptivos, por ejemplo:

```text
docs: ampliar guía de configuración
fix: validar confirmación del proveedor
```

No incluyas tokens, claves ni logs completos en el mensaje o cuerpo del commit.
