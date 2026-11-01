#!/usr/bin/env python3
"""Prepare every SQL constant of the Go repository layer against a live database.

The repository packages keep their SQL as Go string constants named q<Something>
(`qPublish = ...`). PREPARE makes PostgreSQL parse and plan each statement without
running it, which catches unknown tables/columns, wrong enum labels, parameter type
mismatches and missing privileges. Run it as the API role so GRANTs are checked too:

    TEST_DATABASE_URL=postgres://redxtube_app:...@host/redxtube_test python3 db/tools/prepare_queries.py

Exit status is non-zero when any statement fails. Needs `psql` (13+).
"""
import glob
import os
import re
import subprocess
import sys

ROOT = os.path.normpath(os.path.join(os.path.dirname(__file__), '..', '..'))
REPO = os.path.join(ROOT, 'backend', 'internal', 'repository')


def go_consts(text):
    """Return {name: sql} for every `name = <raw strings / identifiers joined by +>` inside const blocks."""
    out, raw = {}, {}
    i, n = 0, len(text)
    lines = text.split('\n')
    in_block = False
    idx = 0
    while idx < len(lines):
        line = lines[idx]
        stripped = line.strip()
        if stripped.startswith('const ('):
            in_block = True
            idx += 1
            continue
        if in_block and stripped == ')':
            in_block = False
            idx += 1
            continue
        m = re.match(r'^\s*(?:const\s+)?(\w+)\s*=\s*(.*)$', line) if (in_block or stripped.startswith('const ')) else None
        if not m:
            idx += 1
            continue
        name, rhs = m.group(1), m.group(2)
        # a raw string may span lines: keep reading until the backticks balance
        while rhs.count('`') % 2 == 1 and idx + 1 < len(lines):
            idx += 1
            rhs += '\n' + lines[idx]
        # `+` continuation onto the next line
        while rhs.rstrip().endswith('+') and idx + 1 < len(lines):
            idx += 1
            rhs += '\n' + lines[idx]
            while rhs.count('`') % 2 == 1 and idx + 1 < len(lines):
                idx += 1
                rhs += '\n' + lines[idx]
        raw[name] = rhs
        idx += 1

    def resolve(name, seen=()):
        if name in out:
            return out[name]
        if name not in raw or name in seen:
            return None
        parts, rhs = [], raw[name]
        for tok in re.finditer(r'`([^`]*)`|"((?:[^"\\]|\\.)*)"|([A-Za-z_]\w*)', rhs):
            if tok.group(1) is not None:
                parts.append(tok.group(1))
            elif tok.group(2) is not None:
                parts.append(tok.group(2))
            else:
                v = resolve(tok.group(3), seen + (name,))
                if v is None:
                    return None
                parts.append(v)
        out[name] = ''.join(parts)
        return out[name]

    for name in raw:
        resolve(name)
    return out


def main():
    url = os.environ.get('TEST_DATABASE_URL') or os.environ.get('DATABASE_URL')
    if not url:
        sys.exit('set TEST_DATABASE_URL (the redxtube_app role on a database built from the migrations)')
    queries = {}
    for path in sorted(glob.glob(os.path.join(REPO, '*.go'))):
        if path.endswith('_test.go'):
            continue
        for name, sql in go_consts(open(path, encoding='utf-8').read()).items():
            if re.match(r'^q[A-Z]', name):
                queries[f'{os.path.basename(path)}:{name}'] = sql
    if not queries:
        sys.exit('no q* constants found')

    script = []
    for key, sql in queries.items():
        script.append(f'\\warn >>> {key}')
        script.append(f'PREPARE s AS {sql.strip().rstrip(";")};')
        script.append('DEALLOCATE s;')
    proc = subprocess.run(['psql', url, '-X', '-q', '-v', 'ON_ERROR_STOP=0'], input='\n'.join(script) + '\n',
                          capture_output=True, text=True)
    failed, current = {}, None
    for line in proc.stderr.splitlines():
        if line.startswith('>>> '):
            current = line[4:]
        elif 'ERROR:' in line and current:
            failed.setdefault(current, []).append(line.split('ERROR:', 1)[1].strip())
    for key in queries:
        print(('FAIL ' if key in failed else 'ok   ') + key + (('  -> ' + '; '.join(failed[key])) if key in failed else ''))
    if proc.returncode not in (0,) and not failed:
        print(proc.stderr)
        sys.exit(1)
    print(f'\n{len(queries) - len(failed)}/{len(queries)} statements prepared')
    sys.exit(1 if failed else 0)


if __name__ == '__main__':
    main()
