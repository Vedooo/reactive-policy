#!/usr/bin/env python3
"""Triage govulncheck JSON output: fail only on what we can actually fix.

govulncheck's exit code lumps together two very different findings:

  * Standard library vulnerabilities, which are fixed by the Go toolchain patch
    release the runner happens to have. They come and go with the runner image,
    not with anything in this repo, so gating on them means CI turns red for
    reasons no commit here can address.

  * Dependency vulnerabilities, which we fix by bumping a module — exactly the
    class that went unnoticed while the scan step was silently broken.

So this script fails the build only for findings that are both reachable from
our own code (govulncheck resolved a call path to a symbol) and outside the
standard library. Everything else is printed and tolerated.

Usage: triage-govulncheck.py <govulncheck-json-file>
"""

import json
import sys


def findings(raw):
    """Yield finding objects from govulncheck's concatenated-JSON output."""
    decoder = json.JSONDecoder()
    i = 0
    while i < len(raw):
        while i < len(raw) and raw[i].isspace():
            i += 1
        if i >= len(raw):
            return
        obj, i = decoder.raw_decode(raw, i)
        if "finding" in obj:
            yield obj["finding"]


def main():
    if len(sys.argv) != 2:
        print("usage: triage-govulncheck.py <govulncheck-json-file>", file=sys.stderr)
        return 2

    with open(sys.argv[1], encoding="utf-8") as fh:
        raw = fh.read()

    blocking, tolerated = {}, {}
    for finding in findings(raw):
        trace = finding.get("trace") or []
        if not trace:
            continue
        top = trace[0]
        module = top.get("module") or "?"
        osv = finding.get("osv") or "?"
        entry = (module, finding.get("fixed_version") or "no fix")

        # A resolved function in the top frame is govulncheck saying it found a
        # real call path, not merely that the package is linked in.
        reachable = bool(top.get("function"))
        if reachable and module != "stdlib":
            blocking[osv] = entry
        else:
            why = "stdlib" if module == "stdlib" else "not reachable"
            tolerated.setdefault(osv, (entry, why))

    if tolerated:
        print(f"Tolerated ({len(tolerated)}): stdlib findings track the runner's Go "
              "patch level; unreachable ones are not called from our code.")
        for osv, ((module, fixed), why) in sorted(tolerated.items()):
            print(f"  - {osv}  {module}  fixed={fixed}  ({why})")

    if not blocking:
        print("\nNo reachable dependency vulnerabilities.")
        return 0

    print(f"\nBlocking ({len(blocking)}): reachable from our code and fixable by "
          "bumping a dependency.")
    for osv, (module, fixed) in sorted(blocking.items()):
        print(f"  - {osv}  {module}  -> {fixed}")
    print("\nBump the modules above, or record why the call path is safe.")
    return 1


if __name__ == "__main__":
    sys.exit(main())
