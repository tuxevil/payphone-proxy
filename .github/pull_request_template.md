## Resumen

<!-- Describe el problema y el cambio observable. -->

## Alcance

- [ ] El cambio está acotado a este pull request.
- [ ] Documenté configuración, migraciones o impacto operativo.
- [ ] Indiqué explícitamente lo que queda fuera de alcance.

## Validación

- [ ] `gofmt -w ./cmd ./internal`
- [ ] `go test ./...`
- [ ] `go test -race ./...`
- [ ] `go vet ./...`
- [ ] `go build ./cmd/payphone-proxy`
- [ ] Validé Docker o CI si el cambio lo requiere.

## Seguridad y privacidad

- [ ] No incluí tokens, claves, `StoreID`, certificados, bases SQLite ni
      datos de pago.
- [ ] Los ejemplos usan placeholders y dominios reservados.
- [ ] No añadí URLs de checkout, logs completos ni información de
      infraestructura.
- [ ] Si encontré una vulnerabilidad, la reporté por el canal privado de
      [SECURITY.md](../SECURITY.md) y no en este pull request.

## Revisión

<!-- Añade notas de compatibilidad, reversión y riesgos conocidos. -->
