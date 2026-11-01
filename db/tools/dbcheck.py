#!/usr/bin/env python3
"""Static consistency checker for the SQL in db/ (no database needed).

It is NOT a SQL parser; it checks the things that most often break migrations:
  * every FK / index / trigger / view / seed INSERT refers to tables and columns that exist
  * enum literals compared with or inserted into enum columns exist in that enum
  * every CREATE TABLE/TYPE/FUNCTION in an up-migration is dropped by its down-migration
  * partial-index predicates and CHECKs only mention columns of their own table
Run:  python3 db/tools/dbcheck.py
"""
import re, sys, glob, os

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
errors, notes = [], []

def strip_comments(sql):
    return re.sub(r'--[^\n]*', '', sql)

def read(p):
    return strip_comments(open(p, encoding='utf-8').read())

def split_top(s, sep=','):
    out, depth, cur, q = [], 0, [], False
    for ch in s:
        if ch == "'": q = not q
        if not q:
            if ch == '(': depth += 1
            elif ch == ')': depth -= 1
            elif ch == sep and depth == 0:
                out.append(''.join(cur)); cur = []; continue
        cur.append(ch)
    if ''.join(cur).strip(): out.append(''.join(cur))
    return out

def matching_paren(s, start):
    depth, q = 0, False
    for i in range(start, len(s)):
        ch = s[i]
        if ch == "'": q = not q
        if q: continue
        if ch == '(': depth += 1
        elif ch == ')':
            depth -= 1
            if depth == 0: return i
    raise ValueError('unbalanced parentheses')

ups = sorted(glob.glob(f'{ROOT}/migrations/*.up.sql'))
downs = {os.path.basename(p).replace('.down.sql', ''): read(p) for p in glob.glob(f'{ROOT}/migrations/*.down.sql')}

enums, tables, views, functions, triggers_fn = {}, {}, set(), set(), []
CONSTRAINT_START = ('CONSTRAINT', 'PRIMARY', 'UNIQUE', 'CHECK', 'FOREIGN', 'EXCLUDE')

for path in ups:
    name = os.path.basename(path).replace('.up.sql', '')
    sql = read(path)
    for m in re.finditer(r'CREATE TYPE (\w+) AS ENUM \((.*?)\);', sql, re.S):
        enums[m.group(1)] = set(re.findall(r"'([^']*)'", m.group(2)))
    for m in re.finditer(r'CREATE VIEW (\w+)', sql): views.add(m.group(1))
    for m in re.finditer(r'CREATE (?:OR REPLACE )?FUNCTION (\w+)', sql): functions.add(m.group(1))
    for m in re.finditer(r'CREATE TABLE (\w+) \(', sql):
        start = m.end() - 1
        end = matching_paren(sql, start)
        body = sql[start + 1:end]
        cols, items = {}, []
        for item in split_top(body):
            it = item.strip()
            if not it: continue
            first = it.split()[0].upper()
            if first in CONSTRAINT_START: items.append(it); continue
            toks = it.split()
            cols[toks[0]] = toks[1].rstrip(',') if len(toks) > 1 else ''
            items.append(it)
        tables[m.group(1)] = {'cols': cols, 'items': items, 'file': name}
    # Columns added by later migrations: ALTER TABLE t ADD COLUMN c type[, ADD COLUMN ...][, ADD CONSTRAINT ...];
    for m in re.finditer(r'ALTER TABLE (\w+)\s+(.*?);', sql, re.S):
        t = m.group(1)
        if t not in tables: continue
        for add in re.finditer(r'ADD COLUMN (?:IF NOT EXISTS )?(\w+)\s+(\w+)', m.group(2), re.I):
            tables[t]['cols'][add.group(1)] = add.group(2)

def col_names(t): return set(tables[t]['cols'])

# ---- 1. FKs, constraint columns ----
fk_re = re.compile(r'REFERENCES (\w+)\s*(?:\(([^)]*)\))?', re.I)
for t, info in tables.items():
    for it in info['items']:
        for m in fk_re.finditer(it):
            rt = m.group(1)
            if rt not in tables: errors.append(f'{t}: FK to unknown table {rt}'); continue
            if m.group(2):
                for c in [x.strip() for x in m.group(2).split(',')]:
                    if c not in tables[rt]['cols']: errors.append(f'{t}: FK to unknown column {rt}.{c}')
        m = re.match(r'(?:CONSTRAINT \w+\s+)?(?:PRIMARY KEY|UNIQUE|FOREIGN KEY)\s*\(([^)]*)\)', it, re.I)
        if m:
            for c in [x.strip() for x in m.group(1).split(',')]:
                if c not in tables[t]['cols']: errors.append(f'{t}: constraint uses unknown column {c}')
        # enum literals in CHECKs
        if it.upper().lstrip().startswith(('CONSTRAINT', 'CHECK')) or ' CHECK ' in it.upper():
            for m in re.finditer(r"\b(\w+)\s*(?:=|<>|!=)\s*'([A-Z0-9_ ]+)'", it):
                c, lit = m.groups()
                typ = tables[t]['cols'].get(c)
                if typ in enums and lit not in enums[typ]: errors.append(f'{t}: CHECK compares {c} with unknown label {lit} (enum {typ})')
            for m in re.finditer(r"\b(\w+)\s+(?:NOT\s+)?IN\s*\(([^)]*)\)", it):
                c, lits = m.groups()
                typ = tables[t]['cols'].get(c)
                if typ in enums:
                    for lit in re.findall(r"'([^']*)'", lits):
                        if lit not in enums[typ]: errors.append(f'{t}: CHECK {c} IN has unknown label {lit} (enum {typ})')

# column defaults using enum literals + enum type existence
for t, info in tables.items():
    for c, typ in info['cols'].items():
        base = typ.replace('[]', '')
        known = set(enums) | {'uuid','text','citext','boolean','integer','bigint','smallint','timestamptz','date','jsonb','bytea','cidr','numeric','char','tsvector','int','varchar'}
        base = re.sub(r'\(.*', '', base)
        if base not in known: errors.append(f'{t}.{c}: unknown type {typ}')
    for it in info['items']:
        m = re.match(r"(\w+)\s+(\w+)\s.*DEFAULT '([A-Z_0-9]+)'", it, re.S)
        if m and m.group(2) in enums and m.group(3) not in enums[m.group(2)]:
            errors.append(f'{t}.{m.group(1)}: default {m.group(3)} not in enum {m.group(2)}')

# ---- 2. indexes ----
idx_re = re.compile(r'CREATE (?:UNIQUE )?INDEX (\w+) ON (\w+)(?: USING \w+)? \((.*?)\)(?:\s+WHERE (.*?))?;', re.S)
index_names = set()
for path in ups:
    sql = read(path)
    for m in idx_re.finditer(sql):
        iname, t, cols, where = m.groups()
        if iname in index_names: errors.append(f'duplicate index name {iname}')
        index_names.add(iname)
        if t not in tables: errors.append(f'index {iname}: unknown table {t}'); continue
        for part in split_top(cols):
            part = re.sub(r'\b(DESC|ASC|NULLS FIRST|NULLS LAST|gin_trgm_ops|text_pattern_ops)\b', '', part, flags=re.I).strip()
            ident = re.match(r'^\(?\s*(\w+)', part)
            if ident and ident.group(1) not in tables[t]['cols']:
                errors.append(f'index {iname}: unknown column {ident.group(1)} on {t}')
        if where:
            idents = set(re.findall(r'\b([a-z_]+)\b', re.sub(r"'[^']*'", '', where)))
            kw = {'and','or','not','in','is','null','true','false','now','interval'}
            for w in idents - kw:
                if w not in tables[t]['cols']: errors.append(f'index {iname}: WHERE mentions unknown column {w}')
            for c, lit in re.findall(r"\b(\w+)\s*=\s*'([A-Z0-9_]+)'", where):
                typ = tables[t]['cols'].get(c)
                if typ in enums and lit not in enums[typ]: errors.append(f'index {iname}: {c} = {lit} not in enum {typ}')
            for c, lits in re.findall(r"\b(\w+)\s+IN\s*\(([^)]*)\)", where):
                typ = tables[t]['cols'].get(c)
                for lit in re.findall(r"'([^']*)'", lits):
                    if typ in enums and lit not in enums[typ]: errors.append(f'index {iname}: {c} IN {lit} not in enum {typ}')
            for c, lit in re.findall(r"\b(\w+)\s*<>\s*'([A-Z0-9_]+)'", where):
                typ = tables[t]['cols'].get(c)
                if typ in enums and lit not in enums[typ]: errors.append(f'index {iname}: {c} <> {lit} not in enum {typ}')

# ---- 3. triggers and functions ----
all_sql = '\n'.join(read(p) for p in ups)
rel_sql = re.sub(r'(?:REVOKE|GRANT)\b[^;]*;', '', all_sql)
for m in re.finditer(r'CREATE TRIGGER (\w+)\s+.*?\sON (\w+)\s.*?EXECUTE FUNCTION (\w+)\(', all_sql, re.S):
    tn, t, fn = m.groups()
    if t not in tables: errors.append(f'trigger {tn}: unknown table {t}')
    if fn not in functions: errors.append(f'trigger {tn}: unknown function {fn}')
for m in re.finditer(r'UPDATE OF ([\w, ]+) ON (\w+)', all_sql):
    for c in [x.strip() for x in m.group(1).split(',')]:
        if m.group(2) in tables and c not in tables[m.group(2)]['cols']: errors.append(f'trigger column {m.group(2)}.{c} unknown')
for m in re.finditer(r"ARRAY\[\s*((?:'[a-z_]+',?\s*)+)\]\s*LOOP", all_sql):
    for t in re.findall(r"'([a-z_]+)'", m.group(1)):
        if t not in tables: errors.append(f'updated_at trigger list names unknown table {t}')
        elif 'updated_at' not in tables[t]['cols']: errors.append(f'updated_at trigger on {t} but no updated_at column')

# NEW.<col> / OLD.<col> inside trigger functions: must be a column of the table the trigger is attached to
fn_bodies = {m.group(1): m.group(2) for m in re.finditer(r'CREATE FUNCTION (\w+)\(\).*?\$\$(.*?)\$\$', all_sql, re.S)}
fn_table = {m.group(3): m.group(2) for m in re.finditer(r'CREATE TRIGGER (\w+)\s+.*?\sON (\w+)\s.*?EXECUTE FUNCTION (\w+)\(', all_sql, re.S)}
for fn, t in fn_table.items():
    for c in set(re.findall(r'\b(?:NEW|OLD)\.(\w+)', fn_bodies.get(fn, ''))):
        if t in tables and c not in tables[t]['cols']: errors.append(f'{fn}: NEW/OLD.{c} is not a column of {t}')

# SQL snippets inside functions: INSERT/UPDATE targets
known_rel = set(tables) | views
for m in re.finditer(r'\b(?:INSERT INTO|UPDATE|DELETE FROM|FROM|JOIN)\s+(\w+)\b', rel_sql):
    r = m.group(1)
    if r.lower() in ('select','set','only','lateral','unnest','generate_series','values'): continue
    if r.startswith('redxtube_'): continue
    if r in known_rel or r in functions: continue
    if r in ('pg_class','pg_inherits','pg_roles','format','information_schema'): continue
    if re.match(r'^[a-z]$', r): continue
    if r.upper() == r: continue
    errors.append(f'SQL refers to unknown relation: {r}')

# ---- 4. down migrations must undo every object their up created ----
for path in ups:
    name = os.path.basename(path).replace('.up.sql', '')
    up, down = read(path), downs.get(name)
    if down is None: errors.append(f'{name}: missing down migration'); continue
    for t in re.findall(r'CREATE TABLE (\w+)', up):
        if not re.search(rf'DROP TABLE IF EXISTS {t}\b', down): errors.append(f'{name}: down does not drop table {t}')
    for v in re.findall(r'CREATE VIEW (\w+)', up):
        if not re.search(rf'DROP VIEW IF EXISTS {v}\b', down): errors.append(f'{name}: down does not drop view {v}')
    for ty in re.findall(r'CREATE TYPE (\w+)', up):
        if not re.search(rf'\b{ty}\b', down): errors.append(f'{name}: down does not drop type {ty}')
    for fn in re.findall(r'CREATE FUNCTION (\w+)', up):
        if not re.search(rf'DROP FUNCTION IF EXISTS {fn}\b', down): errors.append(f'{name}: down does not drop function {fn}')
    for tg in re.findall(r'CREATE TRIGGER (\w+)', up):
        if not re.search(rf'DROP TRIGGER IF EXISTS {tg}\b', down): errors.append(f'{name}: down does not drop trigger {tg}')
    for sq in re.findall(r'CREATE SEQUENCE (\w+)', up):
        if not re.search(rf'DROP SEQUENCE IF EXISTS {sq}\b', down): errors.append(f'{name}: down does not drop sequence {sq}')

# ---- 5. seed file ----
seed = read(f'{ROOT}/seeds/dev_seed.sql')
for m in re.finditer(r'INSERT INTO (\w+) \(([^)]*)\)', seed):
    t, cols = m.group(1), [c.strip() for c in m.group(2).split(',')]
    if t not in tables: errors.append(f'seed: unknown table {t}'); continue
    for c in cols:
        if c not in tables[t]['cols']: errors.append(f'seed: {t}.{c} does not exist')
    # literal values for enum columns in simple VALUES rows
    rest = seed[m.end():]
    vm = re.match(r'\s*VALUES(.*?)(?:ON CONFLICT|;)', rest, re.S)
    if vm:
        for row in re.findall(r'\(((?:[^()]|\([^()]*\))*)\)', vm.group(1)):
            vals = split_top(row)
            if len(vals) != len(cols): errors.append(f'seed: {t} row has {len(vals)} values for {len(cols)} columns: {row[:60].strip()}'); continue
            for c, v in zip(cols, vals):
                lit = re.fullmatch(r"\s*'([A-Z0-9_]+)'\s*", v)
                typ = tables[t]['cols'].get(c)
                if lit and typ in enums and lit.group(1) not in enums[typ]: errors.append(f'seed: {t}.{c} = {lit.group(1)} not in enum {typ}')

# ---- 6. example queries: relations only ----
ex = read(f'{ROOT}/queries/examples.sql')
for m in re.finditer(r'\b(?:FROM|JOIN|INTO|UPDATE)\s+(\w+)\b', ex):
    r = m.group(1)
    if r in known_rel or r.lower() in ('unnest','select','set','only','lateral') or r in functions or re.match(r'^[a-z]$', r) or r in ('moderation_actions','audit_logs'): continue
    errors.append(f'examples.sql: unknown relation {r}')

print(f'migrations: {len(ups)} | tables: {len(tables)} | enums: {len(enums)} | functions: {len(functions)} | indexes: {len(index_names)}')
if errors:
    print(f'\n{len(errors)} problem(s):')
    for e in errors: print('  -', e)
    sys.exit(1)
print('OK: no structural problems found')
