#!/usr/bin/env python3
"""Generate the owned training protocol's Go wire models without importing Python deps."""
import ast
import json
import re
import subprocess
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SOURCE = REPO / 'docs/fireworks-py/fireworks_ai-1.2.39/dist/fireworks_ai-1.2.39/src/fireworks/training/sdk/types'
OUTPUT = REPO / 'training/types/generated.go'
classes, aliases, enums = {}, {}, {}
paths = sorted(SOURCE.glob('*.py')) + sorted((SOURCE / '_pydantic_types').glob('*.py'))
for path in paths:
    for node in ast.parse(path.read_text()).body:
        if isinstance(node, ast.ClassDef) and not node.name.startswith('_'):
            if any(ast.unparse(b) in ('Enum', 'str, Enum') for b in node.bases):
                enums[node.name] = node
            else:
                classes[node.name] = node
        if isinstance(node, ast.AnnAssign) and isinstance(node.target, ast.Name) and ast.unparse(node.annotation) == 'TypeAlias':
            aliases[node.target.id] = node.value
        if isinstance(node, ast.Assign) and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name):
            name = node.targets[0].id
            if name[:1].isupper() and isinstance(node.value, ast.Subscript):
                aliases[name] = node.value
known = classes.keys() | aliases.keys() | enums.keys()

def arguments(node):
    return list(node.slice.elts) if isinstance(node.slice, ast.Tuple) else [node.slice]

def optional(kind):
    return kind if kind.startswith(('[]', 'map[')) or kind == 'any' else '*' + kind

def go_type(node):
    if isinstance(node, ast.Constant) and isinstance(node.value, str):
        try:
            return go_type(ast.parse(node.value, mode='eval').body)
        except SyntaxError:
            return 'any'
    if isinstance(node, ast.Name):
        return {'str':'string', 'int':'int', 'float':'float64', 'bool':'bool', 'Any':'any', 'dict':'map[string]any', 'datetime':'time.Time', 'RoutingMatrixFormat':'string'}.get(node.id, node.id if node.id in known else 'any')
    if isinstance(node, ast.BinOp) and isinstance(node.op, ast.BitOr):
        if isinstance(node.right, ast.Constant) and node.right.value is None:
            return optional(go_type(node.left))
        return 'any'
    if isinstance(node, ast.Subscript):
        name = ast.unparse(node.value)
        args = arguments(node)
        if name in ('Annotated', 'Required'):
            return go_type(args[0])
        if name == 'Optional':
            return optional(go_type(args[0]))
        if name in ('List', 'list', 'Sequence', 'Iterable'):
            return '[]' + go_type(args[0])
        if name in ('Dict', 'dict', 'Mapping'):
            return 'map[string]' + go_type(args[-1])
        if name == 'Literal':
            return 'bool' if isinstance(args[0], ast.Constant) and isinstance(args[0].value, bool) else 'string'
        if name == 'Union':
            values = [x for x in args if not isinstance(x, ast.Constant) or x.value is not None]
            if len(values) == 1:
                return optional(go_type(values[0]))
    return 'any'

def field_name(name):
    return ''.join({'id':'ID', 'url':'URL', 'api':'API', 'ttl':'TTL'}.get(s, s[:1].upper()+s[1:]) for s in name.rstrip('_').split('_'))

def fields(node, seen=None):
    seen = set() if seen is None else seen
    if node.name in seen:
        return {}
    seen.add(node.name)
    result = {}
    for base in node.bases:
        if isinstance(base, ast.Name) and base.id in classes:
            result.update(fields(classes[base.id], seen))
    for statement in node.body:
        if isinstance(statement, ast.AnnAssign) and isinstance(statement.target, ast.Name) and not statement.target.id.startswith('_'):
            result[statement.target.id] = statement
    return result

out = ['// Code generated from Fireworks Python SDK 1.2.39 training types. DO NOT EDIT.', 'package types', 'import "encoding/json"']
for name, node in sorted(aliases.items()):
    if name not in classes and name not in enums:
        out.append(f'type {name} = {go_type(node)}')
for name, node in sorted(enums.items()):
    out.append(f'type {name} string')
    for assignment in node.body:
        if isinstance(assignment, ast.Assign) and isinstance(assignment.value, ast.Constant):
            out.append(f'const {name}{field_name(assignment.targets[0].id.lower())} {name} = {json.dumps(assignment.value.value)}')
for name, node in sorted(classes.items()):
    out.append(f'type {name} struct {{')
    defaults = []
    for wire, statement in fields(node).items():
        kind = go_type(statement.annotation)
        value = statement.value
        if isinstance(value, ast.Call):
            value = next((k.value for k in value.keywords if k.arg == 'default'), None)
        default = value.value if isinstance(value, ast.Constant) else None
        has_default = isinstance(value, ast.Constant) and default is not None
        if has_default and kind in ('bool', 'int', 'float64', 'string'):
            kind = '*' + kind
        field = field_name(wire)
        tag = wire.rstrip('_') + (',omitempty' if statement.value is not None else '')
        out.append(f'{field} {kind} `json:"{tag}"`')
        if has_default and kind.startswith('*'):
            literal = json.dumps(default)
            if kind == '*float64':
                literal = 'float64(' + literal + ')'
            defaults.append(f'if v.{field}==nil {{value:={literal};v.{field}=&value}}')
    out.append('}')
    if defaults:
        out.append(f'func (v {name}) MarshalJSON() ([]byte,error) {{type wire {name};' + ';'.join(defaults) + ';return json.Marshal(wire(v))}')
text = '\n'.join(out) + '\n'
if 'time.Time' in text:
    text = text.replace('import "encoding/json"', 'import ("encoding/json";"time")')
OUTPUT.parent.mkdir(parents=True, exist_ok=True)
OUTPUT.write_text(text)
subprocess.run(['gofmt', '-w', str(OUTPUT)], check=True)
print(f'Generated {len(classes)} training models and {len(aliases)} aliases')
