# Importers run as go-plugin subprocesses

Importers are Go programs that `beancount import` starts and talks to through HashiCorp's go-plugin (gRPC). They return directives as protobuf messages rather than printing Beancount text. We chose a typed, versioned protocol, with `Identify` and `Extract` calls, over the simpler contract where any program prints Beancount text on stdout, even though it means keeping a protobuf schema of the directives in sync with `ast/`. Importers written in other languages, such as beangulp's `extract`, are not supported by `beancount import`. Their output can still be piped into `beancount check`.

## Considered options

- **Any program that prints Beancount text.** This needs no second schema, and existing Python Importers would work unchanged. We rejected it in favor of a typed Go contract.
- **A compile-time registry** in the user's own build of `beancount`. We rejected it because every new Importer would mean a rebuild.

## Consequences

- gRPC and protobuf become dependencies, and the binary grows.
- Version 1 of the protocol carries only Transactions and Balance assertions. Adding more directive kinds means a new protocol version.
