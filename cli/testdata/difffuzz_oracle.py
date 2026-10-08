"""The official tools as one long-lived process, for cli/difffuzz_test.go.

Python takes some 0.4s to start bean-check, so the differential fuzzer asks
this process instead: one JSON request per line on stdin, {"path": ...,
"queries": [...]}, and one JSON answer per line on stdout:

  check    what `bean-check --json` prints for the ledger, or null when
           beancount raises loading it
  format   what `bean-format` prints for it, or null when it raises
  queries  per statement, what `bean-query -f text` prints on stdout, or
           null when beanquery raises running it; null for them all when it
           raises loading the ledger

It calls the functions the tools' entry points call, so the fuzzer confirms
a divergence with the tools themselves before reporting it.
"""

import functools
import io
import json
import sys

# Whatever the libraries print must not reach the protocol.
answers = sys.stdout
sys.stdout = sys.stderr

from beancount import loader
from beancount.ops import validation
from beancount.scripts.format import align_beancount
from beanquery import parser
from beanquery.shell import BQLShell

loader.initialize(use_cache=False)
# The statements are the same for every ledger, and parsing one takes
# beanquery longer than running it.
parser.parse = functools.lru_cache(maxsize=None)(parser.parse)


def check(path):
    # scripts/check.py's main, with --json, which raises encoding an error on
    # a transaction that writes its own lineno (a Decimal): encoded here, so
    # such a ledger is skipped as one beancount raises on.
    _, errors, _ = loader.load_file(
        path, extra_validations=validation.HARDCORE_VALIDATIONS
    )
    return json.loads(json.dumps({
        "errors": [
            {
                "message": error.message,
                "filename": (error.source or {}).get("filename"),
                "lineno": (error.source or {}).get("lineno"),
            }
            for error in errors
        ]
    }))


def bean_format(path):
    # scripts/format.py's main reads the file as click.File does.
    with open(path, encoding="utf-8") as file:
        return align_beancount(file.read())


def queries(path, statements):
    shell = BQLShell("beancount:" + path, io.StringIO(), False, True, "text", False, False)
    return [attempt(query, shell, statement) for statement in statements]


def query(shell, statement):
    shell.outfile = io.StringIO()
    shell.onecmd(statement)
    return shell.outfile.getvalue()


def attempt(function, *args):
    try:
        return function(*args)
    except Exception:
        return None


for line in sys.stdin:
    request = json.loads(line)
    path = request["path"]
    answer = {
        "check": attempt(check, path),
        "format": attempt(bean_format, path),
        "queries": attempt(queries, path, request["queries"]),
    }
    answers.write(json.dumps(answer) + "\n")
    answers.flush()
