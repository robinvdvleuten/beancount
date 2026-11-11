# `diagnostic`

`Positioned`, the shape of every error a loaded ledger reports on a line (kind, message without its Error line, position), and `SeverityError`/`SeverityWarning` for load and validation errors (`SeverityOf`: an error unless the type declares a `Severity()`). Only errors affect exit codes. Imports `ast` for the position, so `ast` cannot import it.
