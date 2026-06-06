#!/usr/bin/env python3
from __future__ import annotations

import ast
import keyword
import os
import re
import sys
from dataclasses import dataclass
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parent.parent
PYTHON_SDK_VERSION = "1.2.0-alpha.76"
DEFAULT_ROOT = REPO_ROOT / f"docs/fireworks-py/python-sdk-{PYTHON_SDK_VERSION}/src/fireworks/types"
ROOT = Path(os.environ.get("FIREWORKS_PY_TYPES_ROOT", DEFAULT_ROOT)).expanduser()
OUT = REPO_ROOT / "types/generated.go"


def require_root() -> None:
    if ROOT.is_dir():
        return

    message = f"""Fireworks Python SDK types root not found: {ROOT}

Set FIREWORKS_PY_TYPES_ROOT to the Python SDK source types directory, for example:
  FIREWORKS_PY_TYPES_ROOT=/path/to/python-sdk-{PYTHON_SDK_VERSION}/src/fireworks/types go generate ./types

Or place the ignored SDK snapshot at:
  {DEFAULT_ROOT}
"""
    sys.stderr.write(message)
    raise SystemExit(1)


@dataclass(frozen=True)
class ClassRef:
    module: str
    class_name: str
    go_name: str


def camel(value: str) -> str:
    parts = re.split(r"[^0-9A-Za-z]+", value)
    out = ""
    for part in parts:
        if not part:
            continue
        if part.lower() == "api":
            out += "API"
        elif part.lower() == "url":
            out += "URL"
        elif part.lower() == "uri":
            out += "URI"
        elif part.lower() == "id":
            out += "ID"
        elif part.lower() == "dpo":
            out += "DPO"
        elif part.lower() == "rlor":
            out += "RLOR"
        elif part.lower() == "lora":
            out += "Lora"
        else:
            out += part[:1].upper() + part[1:]
    if not out:
        out = "Value"
    if out[0].isdigit():
        out = "Field" + out
    if keyword.iskeyword(out):
        out += "Field"
    return out


def module_name(path: Path) -> str:
    return ".".join(path.relative_to(ROOT).with_suffix("").parts)


def module_prefix(module: str) -> str:
    return "".join(camel(part) for part in module.split("."))


def base_file_camel(module: str) -> str:
    return camel(module.split(".")[-1])


def class_go_name(module: str, class_name: str, used: set[str]) -> str:
    prefix = module_prefix(module)
    file_camel = base_file_camel(module)
    if class_name == file_camel or class_name == prefix:
        candidate = prefix
    elif module.startswith("chat.") and class_name == file_camel.removeprefix("Chat"):
        candidate = prefix
    else:
        candidate = prefix + class_name
    original = candidate
    idx = 2
    while candidate in used:
        candidate = f"{original}{idx}"
        idx += 1
    used.add(candidate)
    return candidate


def collect_classes() -> dict[tuple[str, str], ClassRef]:
    used: set[str] = set()
    refs: dict[tuple[str, str], ClassRef] = {}
    for path in sorted(ROOT.rglob("*.py")):
        if path.name == "__init__.py":
            continue
        module = module_name(path)
        tree = ast.parse(path.read_text())
        for node in tree.body:
            if isinstance(node, ast.ClassDef):
                go_name = class_go_name(module, node.name, used)
                refs[(module, node.name)] = ClassRef(module, node.name, go_name)
    return refs


def import_module(current: str, node: ast.ImportFrom) -> str | None:
    if node.module is None:
        return None
    current_parts = current.split(".")[:-1]
    levels_up = max(node.level - 1, 0)
    if levels_up:
        current_parts = current_parts[:-levels_up]
    module_parts = current_parts + node.module.split(".")
    while module_parts and module_parts[0] in {"", "types"}:
        module_parts.pop(0)
    if module_parts and module_parts[0] == "_models":
        return None
    return ".".join(module_parts)


def get_name(node: ast.AST) -> str:
    if isinstance(node, ast.Name):
        return node.id
    if isinstance(node, ast.Attribute):
        return node.attr
    if isinstance(node, ast.Subscript):
        return get_name(node.value)
    if isinstance(node, ast.Constant):
        return str(node.value)
    return ""


def subscript_args(node: ast.Subscript) -> list[ast.AST]:
    value = node.slice
    if isinstance(value, ast.Tuple):
        return list(value.elts)
    return [value]


def is_none(node: ast.AST) -> bool:
    return isinstance(node, ast.Constant) and node.value is None or isinstance(node, ast.Name) and node.id == "None"


def json_alias(value: ast.AST | None) -> str | None:
    if not isinstance(value, ast.Call):
        return None
    for kw in value.keywords:
        if kw.arg == "alias" and isinstance(kw.value, ast.Constant) and isinstance(kw.value.value, str):
            return kw.value.value
    return None


def is_required_annotation(annotation: ast.AST) -> bool:
    return isinstance(annotation, ast.Subscript) and get_name(annotation) == "Required"


def is_total_false_typeddict(node: ast.ClassDef) -> bool:
    has_typeddict = any(get_name(base) == "TypedDict" for base in node.bases)
    if not has_typeddict:
        return False
    for kw in node.keywords:
        if kw.arg == "total" and isinstance(kw.value, ast.Constant) and kw.value.value is False:
            return True
    return False


def go_type(
    annotation: ast.AST,
    *,
    current_module: str,
    imports: dict[str, str],
    refs: dict[tuple[str, str], ClassRef],
) -> tuple[str, bool]:
    name = get_name(annotation)

    if isinstance(annotation, ast.Subscript):
        args = subscript_args(annotation)
        if name in {"Optional"} and args:
            typ, _ = go_type(args[0], current_module=current_module, imports=imports, refs=refs)
            if typ.startswith("[]") or typ.startswith("map[") or typ == "any":
                return typ, True
            return "*" + typ, True
        if name in {"Required"} and args:
            return go_type(args[0], current_module=current_module, imports=imports, refs=refs)
        if name in {"List", "Iterable", "SequenceNotStr"} and args:
            typ, _ = go_type(args[0], current_module=current_module, imports=imports, refs=refs)
            return "[]" + deref(typ), False
        if name in {"Dict", "Mapping"} and len(args) >= 2:
            value_type, _ = go_type(args[1], current_module=current_module, imports=imports, refs=refs)
            return "map[string]" + deref(value_type), False
        if name in {"Union"}:
            non_none = [arg for arg in args if not is_none(arg)]
            if len(non_none) == 1:
                typ, _ = go_type(non_none[0], current_module=current_module, imports=imports, refs=refs)
                if typ.startswith("[]") or typ.startswith("map[") or typ == "any":
                    return typ, True
                return "*" + typ, True
            return "any", True
        if name in {"Literal", "TypeAlias"}:
            return "string", False

    if name in {"str"}:
        return "string", False
    if name in {"int"}:
        return "int", False
    if name in {"float"}:
        return "float64", False
    if name in {"bool"}:
        return "bool", False
    if name in {"object", "Body", "Query", "Headers", "NotGiven", "Omit"}:
        return "any", False
    if name == "datetime":
        return "time.Time", False

    if name in imports:
        imported_module = imports[name]
        ref = refs.get((imported_module, name))
        if ref:
            return ref.go_name, False

    ref = refs.get((current_module, name))
    if ref:
        return ref.go_name, False

    for (module, class_name), ref in refs.items():
        if class_name == name and module == name_to_module_guess(name):
            return ref.go_name, False

    return "any", False


def deref(typ: str) -> str:
    return typ[1:] if typ.startswith("*") else typ


def optional_primitive_param_type(typ: str, omit: bool) -> str:
    if omit and typ in {"bool", "int", "float64"}:
        return "any"
    return typ


def name_to_module_guess(name: str) -> str:
    words = re.findall(r"[A-Z]+(?=[A-Z][a-z]|$)|[A-Z]?[a-z]+|\d+", name)
    return "_".join(w.lower() for w in words)


def field_name(py_name: str) -> str:
    return camel(py_name)


def build_file(refs: dict[tuple[str, str], ClassRef]) -> str:
    lines = [
        "// Code generated by scripts/generate_types.py from the Fireworks Python SDK; DO NOT EDIT.",
        "",
        "package types",
        "",
        'import "time"',
        "",
    ]

    for path in sorted(ROOT.rglob("*.py")):
        if path.name == "__init__.py":
            continue
        module = module_name(path)
        tree = ast.parse(path.read_text())

        imports: dict[str, str] = {}
        for node in tree.body:
            if isinstance(node, ast.ImportFrom):
                imported_module = import_module(module, node)
                if imported_module is None:
                    continue
                for alias in node.names:
                    imports[alias.asname or alias.name] = imported_module

        for node in tree.body:
            if not isinstance(node, ast.ClassDef):
                continue
            ref = refs[(module, node.name)]
            fields: list[str] = []
            total_false = is_total_false_typeddict(node)
            for stmt in node.body:
                if not isinstance(stmt, ast.AnnAssign) or not isinstance(stmt.target, ast.Name):
                    continue
                py_name = stmt.target.id
                if py_name.startswith("_"):
                    continue
                typ, optional = go_type(stmt.annotation, current_module=module, imports=imports, refs=refs)
                tag_name = json_alias(stmt.value) or py_name
                omit = optional or stmt.value is not None or (total_false and not is_required_annotation(stmt.annotation))
                typ = optional_primitive_param_type(typ, omit)
                tag = tag_name + (",omitempty" if omit else "")
                fields.append(f"\t{field_name(py_name)} {typ} `json:\"{tag}\"`")
            if not fields:
                continue
            lines.append(f"// {ref.go_name} mirrors fireworks.types.{module}.{node.name}.")
            lines.append(f"type {ref.go_name} struct {{")
            lines.extend(fields)
            lines.append("}")
            lines.append("")

    return "\n".join(lines).rstrip() + "\n"


def main() -> None:
    require_root()
    refs = collect_classes()
    OUT.parent.mkdir(parents=True, exist_ok=True)
    OUT.write_text(build_file(refs))


if __name__ == "__main__":
    main()
