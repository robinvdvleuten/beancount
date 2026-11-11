# `cli`

A command loads through `ledgerload.Load(ctx, FileOrStdin.Source())` and only decides how to print what it returns, where the official tools' differences belong: `check` prints warnings and errors with source context, `query` errors plain (`loadQueryContext`, which `runOurQuery` calls too, so the parity suite runs the command's loading path), `doctor` nothing. `format` only loads, with `loader.New().Load`.
